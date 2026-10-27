// Package jetstream owns a bounded synchronous protobuf publisher. It does not
// replace a transactional outbox, inbox or application-level idempotency.
package jetstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// ErrClosed reports terminal client closure, including blocked admission.
var ErrClosed = errors.New("jetstream client is closed")

// Client owns one standalone connection and synchronous publisher. No unscoped
// connection, provisioning, consumer or async publication handle is exposed.
type Client struct {
	conn     *nats.Conn
	js       natsjs.JetStream
	stream   string
	prefix   string
	timeout  time.Duration
	maxBytes int
	inflight chan struct{}
	closing  chan struct{}
	closed   chan struct{}
	once     sync.Once
}

// Receipt confirms broker storage, scoped to the configured stream and its
// duplicate window. Duplicate does not prove business effects happened once.
type Receipt struct {
	Stream    string
	Sequence  uint64
	Duplicate bool
}

type operationError struct {
	operation string
	cause     error
}

// initialDialer is used only by the synchronous initial connection. NoReconnect
// and RetryOnFailedConnect(false) prevent later reuse; New clears the closure to
// release the constructor context after connection/handshake finishes.
type initialDialer struct {
	dial func(string, string) (net.Conn, error)
}

// New copies configuration, resolves a token reference and requires an existing
// acknowledged stream. TLS defaults on. This initial standalone implementation
// fails closed on disconnect; recovery/republication belongs to the owner.
func New(ctx context.Context, cfg *configv1.JetStream, application string) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("jetstream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	cfg = proto.CloneOf(cfg)
	connect, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout.AsDuration())
	defer cancel()
	deadline, _ := connect.Deadline()
	budget := time.Until(deadline)
	if budget <= 0 {
		return nil, context.DeadlineExceeded
	}
	tlsConfig, err := connectionTLS(cfg)
	if err != nil {
		return nil, failed(connect, "TLS configuration", err)
	}
	dialer := &initialDialer{}
	var stopHandshake func() bool
	var handshakeClosed <-chan error
	dialer.dial = func(network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(connect, network, address)
		if err != nil {
			return nil, err
		}
		closed := make(chan error, 1)
		handshakeClosed = closed
		// The SDK sets its own handshake deadline after dialing. Preserve the
		// original total budget and cancellation by closing this initial socket.
		stopHandshake = context.AfterFunc(connect, func() { closed <- conn.Close() })
		return conn, nil
	}
	client := &Client{
		stream:   cfg.Stream,
		prefix:   cfg.SubjectPrefix,
		timeout:  cfg.CommandTimeout.AsDuration(),
		maxBytes: int(cfg.MaxMessageBytes),
		inflight: make(chan struct{}, cfg.MaxInflight),
		closing:  make(chan struct{}),
		closed:   make(chan struct{}),
	}
	options := []nats.Option{
		nats.Name(application),
		nats.Timeout(budget),
		nats.SetCustomDialer(dialer),
		nats.NoReconnect(),
		nats.IgnoreDiscoveredServers(),
		nats.RetryOnFailedConnect(false),
		nats.ReconnectBufSize(-1),
		nats.ClosedHandler(func(*nats.Conn) { close(client.closed) }),
	}
	if tlsConfig != nil {
		options = append(options, nats.Secure(tlsConfig))
	}
	if cfg.TokenEnv != "" {
		token, exists := os.LookupEnv(cfg.TokenEnv)
		if !exists || token == "" {
			return nil, errors.New("jetstream token environment reference is missing or empty")
		}
		options = append(options, nats.Token(token))
	}
	client.conn, err = nats.Connect("nats://"+cfg.Address, options...)
	dialer.dial = nil
	if stopHandshake != nil && !stopHandshake() {
		closedErr := <-handshakeClosed
		if errors.Is(closedErr, net.ErrClosed) {
			closedErr = nil
		}
		err = errors.Join(err, connect.Err(), closedErr)
	}
	if err != nil {
		if client.conn != nil {
			client.conn.Close()
			<-client.closed
		}
		return nil, failed(connect, "connect", err)
	}
	client.js, err = natsjs.New(client.conn, natsjs.WithDefaultTimeout(client.timeout))
	if err == nil {
		err = client.Ping(connect)
	}
	if err == nil {
		err = connect.Err()
	}
	if err != nil {
		return nil, errors.Join(failed(connect, "initialize", err), client.Close())
	}
	return client, nil
}

// Publish serializes a typed protobuf message and waits for storage confirmation.
// suffix must end in a version, e.g. order.v1; eventID must remain stable across
// owner retries. Any timeout is ambiguous. This method never retries a publish.
// The message is borrowed and must not be mutated until this call returns.
func (c *Client) Publish(ctx context.Context, suffix, eventID string, message proto.Message) (*Receipt, error) {
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if !validSuffix(suffix) || !validID(eventID) {
		return nil, errors.New("jetstream requires a versioned resource and a bounded event ID")
	}
	if message == nil || !message.ProtoReflect().IsValid() {
		return nil, errors.New("jetstream requires a typed protobuf within max_message_bytes")
	}
	select {
	case c.inflight <- struct{}{}:
		defer func() { <-c.inflight }()
	case <-c.closing:
		return nil, ErrClosed
	case <-bounded.Done():
		return nil, bounded.Err()
	}
	if proto.Size(message) > c.maxBytes {
		return nil, errors.New("jetstream protobuf exceeds max_message_bytes")
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return nil, failed(bounded, "encode", err)
	}
	if len(data) > c.maxBytes {
		return nil, errors.New("jetstream encoded message exceeds max_message_bytes")
	}
	if err := bounded.Err(); err != nil {
		return nil, err
	}
	select {
	case <-c.closing:
		return nil, ErrClosed
	default:
	}
	ack, err := c.js.PublishMsg(bounded, &nats.Msg{
		Subject: c.prefix + "." + suffix,
		Header: nats.Header{
			"Content-Type": []string{"application/protobuf"},
		},
		Data: data,
	}, natsjs.WithMsgID(eventID), natsjs.WithExpectStream(c.stream), natsjs.WithRetryAttempts(0))
	if err != nil {
		return nil, failed(bounded, "publish", err)
	}
	if ack.Stream != c.stream || ack.Sequence == 0 {
		return nil, errors.New("jetstream returned an invalid storage receipt")
	}
	return &Receipt{
		Stream:    ack.Stream,
		Sequence:  ack.Sequence,
		Duplicate: ack.Duplicate,
	}, nil
}

// Ping verifies the configured JetStream exists and allows acknowledged writes;
// it does not create or update broker resources.
func (c *Client) Ping(ctx context.Context) error {
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	stream, err := c.js.Stream(bounded, c.stream)
	if err != nil {
		return failed(bounded, "health check", err)
	}
	info := stream.CachedInfo()
	if info.Config.NoAck || info.Config.Sealed {
		return errors.New("jetstream stream must allow acknowledged writes")
	}
	if !slices.Contains(info.Config.Subjects, c.prefix+".>") {
		return errors.New("jetstream stream must explicitly include the configured namespace")
	}
	return bounded.Err()
}

// Close rejects admission and closes the connection, waiting for its terminal
// callback. Completed publishes already have storage receipts; in-flight writes
// may be ambiguous and must be reconciled by their durable owner.
func (c *Client) Close() error {
	c.once.Do(func() {
		close(c.closing)
		c.conn.Close()
		<-c.closed
	})
	return nil
}

func (c *Client) commandContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, errors.New("jetstream command context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	select {
	case <-c.closing:
		return nil, nil, ErrClosed
	default:
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	return bounded, cancel, nil
}

func (d *initialDialer) Dial(network, address string) (net.Conn, error) {
	if d.dial == nil {
		return nil, ErrClosed
	}
	return d.dial(network, address)
}

func (e *operationError) Error() string { return "jetstream " + e.operation + " failed" }
func (e *operationError) Unwrap() error { return e.cause }

func failed(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	return &operationError{
		operation: operation,
		cause:     errors.Join(err, ctx.Err()),
	}
}

// Validate checks configuration limits without reading credentials or doing I/O.
func Validate(cfg *configv1.JetStream) error { return validate(cfg) }

func validate(cfg *configv1.JetStream) error {
	if cfg == nil {
		return errors.New("jetstream configuration is required")
	}
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" {
		return errors.New("jetstream address must contain TCP host and port")
	}
	if parsed, err := strconv.ParseUint(port, 10, 16); err != nil || parsed == 0 {
		return errors.New("jetstream port must be in 1..65535")
	}
	parts := strings.Split(cfg.SubjectPrefix, ".")
	if len(parts) != 3 || parts[0] != "muskdeer" || !validLabel(parts[1]) || !validLabel(parts[2]) {
		return errors.New("jetstream subject_prefix must be muskdeer.environment.service")
	}
	if !validLabel(cfg.Stream) || cfg.MaxInflight <= 0 || cfg.MaxInflight > 1024 || cfg.MaxMessageBytes <= 0 || cfg.MaxMessageBytes > 1024*1024 {
		return errors.New("jetstream requires a stream, max_inflight in 1..1024 and max_message_bytes in 1..1048576")
	}
	for name, duration := range map[string]*durationpb.Duration{
		"connect_timeout": cfg.ConnectTimeout,
		"command_timeout": cfg.CommandTimeout,
	} {
		if err := duration.CheckValid(); err != nil {
			return errors.New("jetstream " + name + " must be a valid duration")
		}
		if duration.AsDuration() <= 0 || !proto.Equal(durationpb.New(duration.AsDuration()), duration) {
			return errors.New("jetstream " + name + " must be a positive Go duration")
		}
	}
	return nil
}

func validLabel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index := range len(value) {
		char := value[index]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.' || char == ':' {
			continue
		}
		return false
	}
	return true
}

func validSuffix(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(value) > 128 {
		return false
	}
	for _, part := range parts {
		if !validLabel(part) {
			return false
		}
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(parts[len(parts)-1], "v"), 10, 32)
	return strings.HasPrefix(parts[len(parts)-1], "v") && err == nil && version > 0
}

func connectionTLS(cfg *configv1.JetStream) (*tls.Config, error) {
	if cfg.TlsEnabled != nil && !cfg.GetTlsEnabled() {
		if cfg.RootCertFile != "" {
			return nil, errors.New("jetstream root certificate requires TLS")
		}
		return nil, nil
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}
	if cfg.RootCertFile != "" {
		data, err := os.ReadFile(cfg.RootCertFile)
		if err != nil {
			return nil, err
		}
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM(data) {
			return nil, errors.New("jetstream root certificate contains no usable certificates")
		}
	}
	return config, nil
}
