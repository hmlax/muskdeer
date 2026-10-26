// Package redis owns a bounded, namespaced and rebuildable Redis cache.
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// ErrNotFound distinguishes a missing cache entry from dependency failure.
var ErrNotFound = goredis.Nil

// Client owns one standalone cache connection pool. It exposes string values,
// scopes every key, and requires an explicit TTL for writes. It never stores
// authoritative orders, funds or ownership epochs.
type Client struct {
	conn     *goredis.Client
	prefix   string
	timeout  time.Duration
	once     sync.Once
	closeErr error
}

type operationError struct {
	operation string
	cause     error
}

// New copies and validates configuration, resolves credentials, then pings within
// the caller/connect budget. Command and dial retries are disabled explicitly.
// TLS is enabled by default unless the optional flag explicitly disables it.
func New(ctx context.Context, cfg *configv1.Redis, application string) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("redis context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	cfg = proto.CloneOf(cfg)
	var password string
	if cfg.PasswordEnv != "" {
		var exists bool
		password, exists = os.LookupEnv(cfg.PasswordEnv)
		if !exists || password == "" {
			return nil, errors.New("redis password environment reference is missing or empty")
		}
	}
	tlsConfig, err := connectionTLS(cfg)
	if err != nil {
		return nil, &operationError{
			operation: "TLS configuration",
			cause:     err,
		}
	}
	conn := goredis.NewClient(&goredis.Options{
		Addr:                  cfg.Address,
		DB:                    int(cfg.Database),
		Username:              cfg.Username,
		Password:              password,
		ClientName:            application,
		Protocol:              2,
		PoolSize:              int(cfg.PoolSize),
		MaxActiveConns:        int(cfg.PoolSize),
		MaxConcurrentDials:    int(cfg.PoolSize),
		PipelinePoolSize:      -1,
		MaxRetries:            -1,
		DialerRetries:         1,
		DialTimeout:           cfg.ConnectTimeout.AsDuration(),
		ReadTimeout:           cfg.CommandTimeout.AsDuration(),
		WriteTimeout:          cfg.CommandTimeout.AsDuration(),
		PoolTimeout:           cfg.PoolTimeout.AsDuration(),
		ConnMaxIdleTime:       cfg.MaxIdleTime.AsDuration(),
		ConnMaxLifetime:       cfg.MaxLifetime.AsDuration(),
		ContextTimeoutEnabled: true,
		DisableIdentity:       true,
		TLSConfig:             tlsConfig,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
	})
	client := &Client{
		conn:    conn,
		prefix:  cfg.KeyPrefix,
		timeout: cfg.CommandTimeout.AsDuration(),
	}
	connect, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout.AsDuration())
	defer cancel()
	if err := conn.Ping(connect).Err(); err != nil {
		return nil, errors.Join(failure(connect, "connect", err), client.Close())
	}
	if err := connect.Err(); err != nil {
		return nil, errors.Join(failure(connect, "connect", err), client.Close())
	}
	return client, nil
}

// Get reads a relative resource key; missing values return ErrNotFound.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return "", err
	}
	defer cancel()
	scoped, err := c.key(key)
	if err != nil {
		return "", err
	}
	value, err := c.conn.Get(bounded, scoped).Result()
	if errors.Is(err, goredis.Nil) {
		return "", ErrNotFound
	}
	return value, failure(bounded, "get", err)
}

// Set writes a string with a positive, whole-millisecond TTL. A timeout does not
// prove the server rejected the write; this client does not replay it.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl < time.Millisecond || ttl%time.Millisecond != 0 {
		return errors.New("redis cache TTL must be positive whole milliseconds")
	}
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	scoped, err := c.key(key)
	if err != nil {
		return err
	}
	return failure(bounded, "set", c.conn.Set(bounded, scoped, value, ttl).Err())
}

// Delete invalidates one relative key. Deleting an absent cache key is successful.
func (c *Client) Delete(ctx context.Context, key string) error {
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	scoped, err := c.key(key)
	if err != nil {
		return err
	}
	return failure(bounded, "delete", c.conn.Del(bounded, scoped).Err())
}

// Ping checks the current cache dependency within the command budget.
func (c *Client) Ping(ctx context.Context) error {
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return failure(bounded, "health check", c.conn.Ping(bounded).Err())
}

// Stats returns a pool snapshot; the client exposes no unscoped command handle.
func (c *Client) Stats() *goredis.PoolStats { return c.conn.PoolStats() }

// Close releases all owned SDK pools and preserves the first close result.
func (c *Client) Close() error {
	c.once.Do(func() { c.closeErr = failure(context.Background(), "close", c.conn.Close()) })
	return c.closeErr
}

func (c *Client) commandContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, errors.New("redis command context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	return bounded, cancel, nil
}

func (c *Client) key(relative string) (string, error) {
	if relative == "" {
		return "", errors.New("redis resource key is required")
	}
	return c.prefix + ":" + relative, nil
}

func (e *operationError) Error() string { return "redis " + e.operation + " failed" }
func (e *operationError) Unwrap() error { return e.cause }

func failure(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	cause := ctx.Err()
	// A socket deadline can fire just before the context timer publishes Err.
	// Preserve the expired caller budget without misclassifying earlier I/O
	// failures or timeouts that do not belong to this command's deadline.
	var networkError net.Error
	deadline, bounded := ctx.Deadline()
	if cause == nil && bounded && !time.Now().Before(deadline) && errors.As(err, &networkError) && networkError.Timeout() {
		cause = context.DeadlineExceeded
	}
	return &operationError{
		operation: operation,
		cause:     errors.Join(err, cause),
	}
}

func validate(cfg *configv1.Redis) error {
	if cfg == nil {
		return errors.New("redis configuration is required")
	}
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" {
		return errors.New("redis address must contain TCP host and port")
	}
	if parsed, err := strconv.ParseUint(port, 10, 16); err != nil || parsed == 0 {
		return errors.New("redis port must be in 1..65535")
	}
	if cfg.Database < 0 || cfg.PoolSize <= 0 || cfg.PoolSize > 1024 {
		return errors.New("redis requires nonnegative database and pool_size in 1..1024")
	}
	parts := strings.Split(cfg.KeyPrefix, ":")
	if len(parts) < 2 || len(cfg.KeyPrefix) > 128 {
		return errors.New("redis key_prefix must include environment and service segments")
	}
	for _, part := range parts {
		if part == "" {
			return errors.New("redis key_prefix segments must not be empty")
		}
		for _, char := range part {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.' {
				continue
			}
			return errors.New("redis key_prefix permits ASCII letters, digits, dot, underscore, hyphen and colon")
		}
	}
	for name, value := range map[string]*durationpb.Duration{
		"connect_timeout": cfg.ConnectTimeout,
		"command_timeout": cfg.CommandTimeout,
		"pool_timeout":    cfg.PoolTimeout,
		"max_idle_time":   cfg.MaxIdleTime,
		"max_lifetime":    cfg.MaxLifetime,
	} {
		if err := value.CheckValid(); err != nil {
			return errors.New("redis " + name + " must be a valid duration")
		}
		if value.AsDuration() <= 0 || !proto.Equal(durationpb.New(value.AsDuration()), value) {
			return errors.New("redis " + name + " must be a positive Go duration")
		}
	}
	return nil
}

func connectionTLS(cfg *configv1.Redis) (*tls.Config, error) {
	if cfg.TlsEnabled != nil && !cfg.GetTlsEnabled() {
		if cfg.RootCertFile != "" {
			return nil, errors.New("redis root certificate requires TLS")
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
			return nil, errors.New("redis root certificate contains no usable certificates")
		}
	}
	return config, nil
}
