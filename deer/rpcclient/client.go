// Package rpcclient owns bounded, discovered and authenticated unary RPC calls.
package rpcclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

const (
	maximumMessageBytes = 16 * 1024 * 1024
	missingProvider     = "RPC target has no healthy provider"
	streamUnsupported   = "RPC factory currently supports unary calls only"
)

// ErrClosed reports terminal factory closure.
var ErrClosed = errors.New("RPC client factory is closed")

// Discovery supplies fresh endpoints; failures must not return stale membership.
type Discovery interface {
	GetService(context.Context, string) ([]nacos.Instance, error)
}

// Target explicitly distinguishes a stateless service from a required instance.
// A specified instance that is absent never falls back to a different owner.
type Target struct {
	Service    string
	InstanceID string
}

// Factory owns per-call connections and cancels/joins admitted work on Close.
// It retains no caller context, endpoint cache, resolver watch or background loop.
type Factory struct {
	cfg       *configv1.GRPCClients
	discovery Discovery
	security  map[string]*tls.Config
	slots     chan struct{}
	closing   chan struct{}
	next      atomic.Uint64
	mu        sync.Mutex
	active    map[uint64]context.CancelFunc
	sequence  uint64
	closed    bool
	wait      sync.WaitGroup
	once      sync.Once
}

type connection struct {
	factory *Factory
	target  Target
}

// New validates and snapshots credentials without discovering or opening a socket.
func New(ctx context.Context, cfg *configv1.GRPCClients, service string, discovery Discovery) (*Factory, error) {
	if ctx == nil {
		return nil, errors.New("RPC factory context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	if discovery == nil {
		return nil, errors.New("RPC factory requires discovery")
	}
	cfg = proto.CloneOf(cfg)
	security := make(map[string]*tls.Config, len(cfg.AllowedServices))
	for _, peer := range cfg.AllowedServices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := security[peer]; exists {
			return nil, errors.New("RPC factory contains a duplicate allowed service")
		}
		identity, err := rpcsecurity.Client(cfg.Mtls, service, peer)
		if err != nil {
			return nil, err
		}
		security[peer] = identity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Factory{
		cfg:       cfg,
		discovery: discovery,
		security:  security,
		slots:     make(chan struct{}, cfg.MaxInflight),
		closing:   make(chan struct{}),
		active:    make(map[uint64]context.CancelFunc),
	}, nil
}

// Validate checks limits and references without filesystem or network I/O.
func Validate(cfg *configv1.GRPCClients) error {
	if cfg == nil || cfg.Mtls == nil || cfg.Mtls.CertificateFile == "" || cfg.Mtls.PrivateKeyFile == "" || cfg.Mtls.RootCertFile == "" {
		return errors.New("RPC factory requires mutual TLS file references")
	}
	if cfg.MaxInflight < 1 || cfg.MaxInflight > 64 || cfg.MaxRequestBytes < 1 || cfg.MaxRequestBytes > maximumMessageBytes || cfg.MaxResponseBytes < 1 || cfg.MaxResponseBytes > maximumMessageBytes || len(cfg.AllowedServices) < 1 || len(cfg.AllowedServices) > 16 {
		return errors.New("RPC factory requires 1..64 inflight calls, 1..16 services and message bounds in 1..16 MiB")
	}
	if !validDuration(cfg.ConnectTimeout) || !validDuration(cfg.RequestTimeout) || cfg.ConnectTimeout.AsDuration() > cfg.RequestTimeout.AsDuration() {
		return errors.New("RPC factory requires positive timeouts up to one minute, connect within request budget")
	}
	return nil
}

// Client returns a typed-client-compatible unary facade without network I/O.
// Each Invoke discovers again; the facade never exposes an owned raw connection.
func (f *Factory) Client(target Target) (grpc.ClientConnInterface, error) {
	if _, exists := f.security[target.Service]; !exists || !validInstanceID(target.InstanceID) {
		return nil, errors.New("RPC target requires an allowed service and bounded instance identity")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, ErrClosed
	}
	return &connection{
		factory: f,
		target:  target,
	}, nil
}

// Close prevents admission, cancels and joins all owned calls. It is idempotent.
// Discovery is borrowed and must be closed by its owner after this factory.
func (f *Factory) Close() error {
	f.once.Do(func() {
		f.mu.Lock()
		f.closed = true
		close(f.closing)
		for _, cancel := range f.active {
			cancel()
		}
		f.mu.Unlock()
		f.wait.Wait()
	})
	return nil
}

// Health checks local factory ownership. Remote providers are checked afresh by
// Invoke, so readiness does not invent a persistent connection or cached route.
func (f *Factory) Health() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrClosed
	}
	return nil
}

func (c *connection) Invoke(ctx context.Context, method string, request, reply any, options ...grpc.CallOption) (result error) {
	f := c.factory
	bounded, end, err := f.begin(ctx)
	if err != nil {
		return contextualError(ctx, err)
	}
	defer end()
	defer func() { result = contextualError(bounded, result) }()
	message, typedRequest := request.(proto.Message)
	_, typedReply := reply.(proto.Message)
	if !typedRequest || !typedReply {
		return status.Error(codes.InvalidArgument, "RPC factory requires typed protobuf messages")
	}
	if proto.Size(message) > int(f.cfg.MaxRequestBytes) {
		return status.Error(codes.ResourceExhausted, "RPC request exceeds the configured size limit")
	}
	instances, err := f.discovery.GetService(bounded, c.target.Service)
	if err != nil {
		return fmt.Errorf("discover RPC target: %w", err)
	}
	instance, err := f.selectInstance(c.target, instances)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: f.cfg.ConnectTimeout.AsDuration()}
	conn, err := grpc.NewClient("passthrough:///"+instance.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(f.security[c.target.Service])),
		grpc.WithNoProxy(), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", address)
		}),
	)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, conn.Close()) }()
	connect, cancel := context.WithTimeout(bounded, f.cfg.ConnectTimeout.AsDuration())
	defer cancel()
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if state == connectivity.Shutdown || !conn.WaitForStateChange(connect, state) {
			return contextualError(connect, status.Error(codes.Unavailable, "RPC transport did not become ready"))
		}
	}
	// Pin these after caller options; callers cannot raise payload caps.
	options = append(options, grpc.MaxRetryRPCBufferSize(0),
		grpc.MaxCallSendMsgSize(int(f.cfg.MaxRequestBytes)),
		grpc.MaxCallRecvMsgSize(int(f.cfg.MaxResponseBytes)), grpc.WaitForReady(false))
	return invokeOnce(bounded, conn, method, request, reply, options...)
}

func invokeOnce(ctx context.Context, conn *grpc.ClientConn, method string, request, reply any, options ...grpc.CallOption) error {
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{}, method, options...)
	if err != nil {
		return err
	}
	// Context deliberately commits the established stream before SendMsg. Its
	// documented side effect disables subsequent client-side retries, including
	// transparent retries, independently of WithDisableRetry. No returned peer
	// context is used; pre-stream transport setup cannot replay a business body.
	_ = stream.Context()
	if err := stream.SendMsg(request); err != nil && err != io.EOF {
		return err
	}
	if err := stream.CloseSend(); err != nil && err != io.EOF {
		return err
	}
	if err := stream.RecvMsg(reply); err != nil {
		return err
	}
	if err := stream.RecvMsg(reply); err != io.EOF {
		if err == nil {
			return status.Error(codes.Internal, "unary RPC returned multiple response messages")
		}
		return err
	}
	return nil
}

func (*connection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, streamUnsupported)
}

func (f *Factory) selectInstance(target Target, instances []nacos.Instance) (nacos.Instance, error) {
	if len(instances) == 0 {
		return nacos.Instance{}, status.Error(codes.Unavailable, missingProvider)
	}
	if len(instances) > 1024 {
		return nacos.Instance{}, errors.New("discovery exceeded the provider limit")
	}
	identities := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if instance.Service != target.Service {
			return nacos.Instance{}, errors.New("discovery returned a different service")
		}
		host, port, err := net.SplitHostPort(instance.Address)
		number, portErr := strconv.ParseUint(port, 10, 16)
		if err != nil || net.ParseIP(host) == nil || portErr != nil || number == 0 {
			return nacos.Instance{}, errors.New("discovery returned an invalid literal endpoint")
		}
		if _, duplicate := identities[instance.ID]; duplicate || instance.ID == "" || !validInstanceID(instance.ID) {
			return nacos.Instance{}, errors.New("discovery returned an invalid or duplicate identity")
		}
		identities[instance.ID] = struct{}{}
	}
	if target.InstanceID != "" {
		for _, instance := range instances {
			if instance.ID == target.InstanceID {
				return instance, nil
			}
		}
		return nacos.Instance{}, status.Error(codes.Unavailable, missingProvider)
	}
	return instances[(f.next.Add(1)-1)%uint64(len(instances))], nil
}

func (f *Factory) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("RPC context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, f.cfg.RequestTimeout.AsDuration())
	select {
	case f.slots <- struct{}{}:
	case <-f.closing:
		cancel()
		return nil, nil, ErrClosed
	case <-bounded.Done():
		cancel()
		return nil, nil, bounded.Err()
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		cancel()
		<-f.slots
		return nil, nil, ErrClosed
	}
	f.sequence++
	id := f.sequence
	f.active[id] = cancel
	f.wait.Add(1)
	f.mu.Unlock()
	return bounded, func() {
		cancel()
		f.mu.Lock()
		delete(f.active, id)
		f.mu.Unlock()
		<-f.slots
		f.wait.Done()
	}, nil
}

func contextualError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return errors.Join(status.FromContextError(ctx.Err()).Err(), ctx.Err(), err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(status.FromContextError(context.DeadlineExceeded).Err(), err)
	}
	if errors.Is(err, context.Canceled) {
		return errors.Join(status.FromContextError(context.Canceled).Err(), err)
	}
	return err
}

func validDuration(value *durationpb.Duration) bool {
	return value.CheckValid() == nil && value.AsDuration() > 0 && value.AsDuration() <= time.Minute && proto.Equal(durationpb.New(value.AsDuration()), value)
}

func validInstanceID(id string) bool {
	if len(id) > 128 {
		return false
	}
	return strings.IndexFunc(id, func(ch rune) bool {
		return ch != '-' && ch != '_' && ch != '.' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9')
	}) == -1
}
