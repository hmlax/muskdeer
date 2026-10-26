// Package nacos owns context-bound, authenticated Nacos naming requests without
// process-global SDK state or a local instance cache.
package nacos

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

const (
	maxResponseBytes = 1024 * 1024
	maxInstances     = 1024
	readinessService = "deer-readiness.grpc"
	loginPath        = "/nacos/v1/auth/login"
	instanceListPath = "/nacos/v2/ns/instance/list"
)

// ErrClosed reports terminal closure; a closed client never opens connections.
var ErrClosed = errors.New("nacos client is closed")

// Client owns one HTTP transport, bounded operations and a private expiring auth
// token. Close cancels and joins operations before releasing idle connections.
type Client struct {
	cfg       *configv1.Nacos
	base      string
	http      *http.Client
	transport *http.Transport
	slots     chan struct{}
	closing   chan struct{}
	authGate  chan struct{}
	token     atomic.Pointer[authToken]
	mu        sync.Mutex
	active    map[uint64]context.CancelFunc
	nextID    uint64
	closed    bool
	wait      sync.WaitGroup
	once      sync.Once
}

// Instance is a currently healthy, enabled gRPC endpoint in the configured scope.
// Naming membership is not execution ownership or authorization to trade.
type Instance struct {
	ID      string
	Service string
	Version string
	Address string
}

// APIError exposes only safe status/code information, with an inspectable cause.
type APIError struct {
	HTTPStatus int
	Code       int
	operation  string
	cause      error
}

type authToken struct {
	value   string
	expires time.Time
}

type loginResponse struct {
	AccessToken string `json:"accessToken"`
	TokenTTL    int64  `json:"tokenTtl"`
}

type instanceMetadata struct {
	ID      string `json:"deer_instance_id"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type wireInstance struct {
	IP       string           `json:"ip"`
	Port     uint32           `json:"port"`
	Healthy  bool             `json:"healthy"`
	Enabled  bool             `json:"enabled"`
	Weight   json.Number      `json:"weight"`
	Cluster  string           `json:"clusterName"`
	Metadata instanceMetadata `json:"metadata"`
}

type instancesResponse struct {
	Code int `json:"code"`
	Data *struct {
		Name  string         `json:"name"`
		Hosts []wireInstance `json:"hosts"`
	} `json:"data"`
}

// New validates/copies configuration and probes authenticated naming within the
// caller/connect budget. The configured namespace must be explicitly provisioned.
func New(ctx context.Context, cfg *configv1.Nacos) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("nacos context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	cfg = proto.CloneOf(cfg)
	if password, exists := os.LookupEnv(cfg.PasswordEnv); !exists || password == "" {
		return nil, errors.New("nacos password environment reference is missing or empty")
	}
	secure, err := connectionTLS(cfg)
	if err != nil {
		return nil, failure(ctx, "TLS configuration", err)
	}
	scheme := "https"
	if secure == nil {
		scheme = "http"
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: cfg.ConnectTimeout.AsDuration()}).DialContext,
		TLSClientConfig:       secure,
		TLSHandshakeTimeout:   cfg.ConnectTimeout.AsDuration(),
		ResponseHeaderTimeout: cfg.CommandTimeout.AsDuration(),
		MaxConnsPerHost:       int(cfg.MaxInflight),
		MaxIdleConns:          int(cfg.MaxInflight),
		MaxIdleConnsPerHost:   int(cfg.MaxInflight),
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    true,
	}
	client := &Client{
		cfg:       cfg,
		base:      scheme + "://" + cfg.Address,
		transport: transport,
		http: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		slots:    make(chan struct{}, cfg.MaxInflight),
		closing:  make(chan struct{}),
		authGate: make(chan struct{}, 1),
		active:   make(map[uint64]context.CancelFunc),
	}
	connect, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout.AsDuration())
	defer cancel()
	if err := client.Ping(connect); err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return client, nil
}

// GetService returns a fresh snapshot of serviceName (including its .grpc suffix).
// It filters unhealthy/disabled/zero-weight instances even when server protection
// thresholds return them. All returned fields are copied from typed wire values.
func (c *Client) GetService(ctx context.Context, serviceName string) ([]Instance, error) {
	bounded, end, err := c.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	if !validIdentifier(serviceName) || !strings.HasSuffix(serviceName, ".grpc") {
		return nil, errors.New("nacos requires a bounded gRPC service name")
	}
	token, err := c.authenticate(bounded)
	if err != nil {
		return nil, failure(bounded, "authenticate", err)
	}
	data, err := c.request(bounded, http.MethodGet, instanceListPath, url.Values{
		"namespaceId": []string{c.cfg.NamespaceId},
		"groupName":   []string{c.cfg.Group},
		"clusterName": []string{c.cfg.Cluster},
		"serviceName": []string{serviceName},
		"healthyOnly": []string{"true"},
	}, token.value)
	if err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && (apiError.HTTPStatus == http.StatusUnauthorized || apiError.HTTPStatus == http.StatusForbidden) {
			c.token.CompareAndSwap(token, nil)
		}
		return nil, failure(bounded, "lookup", err)
	}
	var response instancesResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, failure(bounded, "decode instances", err)
	}
	if response.Code != 0 {
		return nil, &APIError{
			Code:      response.Code,
			operation: "lookup",
		}
	}
	if response.Data == nil || len(response.Data.Hosts) > maxInstances || response.Data.Name != "" && response.Data.Name != c.cfg.Group+"@@"+serviceName {
		return nil, errors.New("nacos returned an invalid instance snapshot")
	}
	instances := make([]Instance, 0, len(response.Data.Hosts))
	identities := make(map[string]struct{}, len(response.Data.Hosts))
	for _, host := range response.Data.Hosts {
		if len(host.Weight) > 64 {
			return nil, errors.New("nacos returned an invalid instance weight")
		}
		weight, err := decimal.NewFromString(string(host.Weight))
		if err != nil || !weight.IsPositive() || !host.Healthy || !host.Enabled || host.Cluster != c.cfg.Cluster || host.Metadata.Kind != "grpc" {
			continue
		}
		ip := net.ParseIP(host.IP)
		if ip == nil || ip.IsUnspecified() || host.Port == 0 || host.Port > 65535 || !validIdentifier(host.Metadata.ID) {
			return nil, errors.New("nacos returned an invalid gRPC endpoint")
		}
		if _, exists := identities[host.Metadata.ID]; exists {
			return nil, errors.New("nacos returned duplicate Deer instance identities")
		}
		identities[host.Metadata.ID] = struct{}{}
		instances = append(instances, Instance{
			ID:      host.Metadata.ID,
			Service: serviceName,
			Version: host.Metadata.Version,
			Address: net.JoinHostPort(host.IP, strconv.FormatUint(uint64(host.Port), 10)),
		})
	}
	return instances, bounded.Err()
}

// Ping checks current scoped authentication/naming without requiring a provider.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.GetService(ctx, readinessService)
	return err
}

// Close prevents new admission, cancels/joins operations and releases connections.
func (c *Client) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.closing)
		for _, cancel := range c.active {
			cancel()
		}
		c.mu.Unlock()
		c.wait.Wait()
		c.transport.CloseIdleConnections()
		c.token.Store(nil)
	})
	return nil
}

func (c *Client) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("nacos command context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.cfg.CommandTimeout.AsDuration())
	select {
	case c.slots <- struct{}{}:
	case <-c.closing:
		cancel()
		return nil, nil, ErrClosed
	case <-bounded.Done():
		cancel()
		return nil, nil, bounded.Err()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cancel()
		<-c.slots
		return nil, nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	c.active[id] = cancel
	c.wait.Add(1)
	c.mu.Unlock()
	return bounded, func() {
		cancel()
		c.mu.Lock()
		delete(c.active, id)
		c.mu.Unlock()
		<-c.slots
		c.wait.Done()
	}, nil
}

func (c *Client) authenticate(ctx context.Context) (*authToken, error) {
	if token := c.token.Load(); token != nil && time.Now().Before(token.expires) {
		return token, nil
	}
	select {
	case c.authGate <- struct{}{}:
		defer func() { <-c.authGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if token := c.token.Load(); token != nil && time.Now().Before(token.expires) {
		return token, nil
	}
	password, exists := os.LookupEnv(c.cfg.PasswordEnv)
	if !exists || password == "" {
		return nil, errors.New("nacos password reference is missing or empty")
	}
	data, err := c.request(ctx, http.MethodPost, loginPath, url.Values{
		"username": []string{c.cfg.Username},
		"password": []string{password},
	}, "")
	if err != nil {
		return nil, err
	}
	var response loginResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if response.AccessToken == "" || len(response.AccessToken) > 8192 || response.TokenTTL <= 0 || response.TokenTTL > 86400 {
		return nil, errors.New("nacos returned invalid authentication lifetime")
	}
	token := &authToken{
		value:   response.AccessToken,
		expires: time.Now().Add(time.Duration(response.TokenTTL) * time.Second * 4 / 5),
	}
	c.token.Store(token)
	return token, nil
}

func (c *Client) request(ctx context.Context, method, path string, values url.Values, token string) (data []byte, err error) {
	address := c.base + path
	var body io.Reader
	if method == http.MethodGet {
		address += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return nil, &APIError{
			HTTPStatus: response.StatusCode,
			operation:  "request",
		}
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if len(data) > maxResponseBytes {
		return nil, errors.New("nacos response exceeds the byte limit")
	}
	return data, err
}

func (e *APIError) Error() string { return "nacos " + e.operation + " failed" }
func (e *APIError) Unwrap() error { return e.cause }

func failure(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := &APIError{
		operation: operation,
		cause:     errors.Join(err, ctx.Err()),
	}
	var original *APIError
	if errors.As(err, &original) {
		wrapped.HTTPStatus = original.HTTPStatus
		wrapped.Code = original.Code
	}
	return wrapped
}

func validate(cfg *configv1.Nacos) error {
	if cfg == nil {
		return errors.New("nacos configuration is required")
	}
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" || strings.ContainsAny(host, "/@?#\\ \t\r\n") {
		return errors.New("nacos address must contain TCP host and port")
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return errors.New("nacos port must be in 1..65535")
	}
	for _, value := range []string{cfg.NamespaceId, cfg.Group, cfg.Cluster, cfg.Username, cfg.PasswordEnv} {
		if !validIdentifier(value) {
			return errors.New("nacos requires explicit bounded scope and credential references")
		}
	}
	if strings.EqualFold(cfg.NamespaceId, "public") || cfg.MaxInflight <= 0 || cfg.MaxInflight > 64 {
		return errors.New("nacos requires an environment namespace and max_inflight in 1..64")
	}
	for name, duration := range map[string]*durationpb.Duration{
		"connect_timeout": cfg.ConnectTimeout,
		"command_timeout": cfg.CommandTimeout,
	} {
		if err := duration.CheckValid(); err != nil {
			return errors.New("nacos " + name + " must be a valid duration")
		}
		if duration.AsDuration() <= 0 || !proto.Equal(durationpb.New(duration.AsDuration()), duration) {
			return errors.New("nacos " + name + " must be a positive Go duration")
		}
	}
	return nil
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func connectionTLS(cfg *configv1.Nacos) (*tls.Config, error) {
	if cfg.TlsEnabled != nil && !cfg.GetTlsEnabled() {
		if cfg.RootCertFile != "" {
			return nil, errors.New("nacos root certificate requires TLS")
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
			return nil, errors.New("nacos root certificate contains no usable certificates")
		}
	}
	return config, nil
}
