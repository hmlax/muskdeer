// Package postgres owns a bounded PostgreSQL pool and its GORM adapter.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// TLS modes accepted by the connection configuration.
const (
	SSLModeVerifyFull = "verify-full"
	SSLModeRequire    = "require"
	SSLModeDisable    = "disable"
)

// Client owns one pool. Business repositories borrow GORM and must explicitly
// attach WithContext(ctx).Model(&generatedModel{}) to every data operation.
type Client struct {
	db       *gorm.DB
	pool     *sql.DB
	timeout  time.Duration
	once     sync.Once
	closeErr error
}

// operationError preserves the cause for errors.Is/As without printing a driver
// configuration or DSN into startup/log output.
type operationError struct {
	operation string
	cause     error
}

// New validates immutable settings, resolves the password reference, configures
// the pool, and pings under both the caller and configured connection deadline.
// It does not migrate schema, open a transaction, or log SQL/credentials.
func New(ctx context.Context, cfg *configv1.Postgres, application string) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("postgres context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	cfg = proto.CloneOf(cfg)
	password, exists := os.LookupEnv(cfg.PasswordEnv)
	if !exists || password == "" {
		return nil, errors.New("postgres password environment reference is missing or empty")
	}
	parsed, err := connectionConfig(cfg, password, application)
	if err != nil {
		return nil, &operationError{operation: "configuration", cause: err}
	}
	pool := stdlib.OpenDB(*parsed)
	pool.SetMaxOpenConns(int(cfg.MaxOpenConns))
	pool.SetMaxIdleConns(int(cfg.MaxIdleConns))
	pool.SetConnMaxLifetime(cfg.MaxLifetime.AsDuration())
	pool.SetConnMaxIdleTime(cfg.MaxIdleTime.AsDuration())
	rollback := func(operation string, cause error) (*Client, error) {
		return nil, errors.Join(&operationError{
			operation: operation,
			cause:     cause,
		}, closePool(pool))
	}
	connect, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout.AsDuration())
	defer cancel()
	if err := pool.PingContext(connect); err != nil {
		return rollback("connect", err)
	}
	db, err := gorm.Open(postgresdriver.New(postgresdriver.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return rollback("gorm initialization", err)
	}
	if err := connect.Err(); err != nil {
		return rollback("connect", err)
	}
	return &Client{
		db:      db,
		pool:    pool,
		timeout: cfg.ConnectTimeout.AsDuration(),
	}, nil
}

// GORM returns a borrowed adapter. The Client owns its underlying pool lifetime.
func (c *Client) GORM() *gorm.DB { return c.db }

// Stats returns a point-in-time pool snapshot for health and metrics.
func (c *Client) Stats() sql.DBStats { return c.pool.Stats() }

// Ping checks live connectivity with a bounded deadline; it can recover after a
// temporary outage without constructing another client or changing global state.
func (c *Client) Ping(ctx context.Context) error {
	if ctx == nil {
		return errors.New("postgres health context is required")
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.pool.PingContext(bounded); err != nil {
		return &operationError{operation: "health check", cause: err}
	}
	return nil
}

// Close releases the owned pool at most once and preserves its close result.
func (c *Client) Close() error {
	c.once.Do(func() { c.closeErr = closePool(c.pool) })
	return c.closeErr
}

func (e *operationError) Error() string { return "postgres " + e.operation + " failed" }
func (e *operationError) Unwrap() error { return e.cause }

func closePool(pool *sql.DB) error {
	if err := pool.Close(); err != nil {
		return &operationError{operation: "close", cause: err}
	}
	return nil
}

// Validate checks configuration limits without reading credentials or doing I/O.
func Validate(cfg *configv1.Postgres) error { return validate(cfg) }

func validate(cfg *configv1.Postgres) error {
	if cfg == nil {
		return errors.New("postgres configuration is required")
	}
	for _, value := range []string{cfg.Host, cfg.Database, cfg.Username, cfg.PasswordEnv} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return errors.New("postgres host, database, username and password_env are required")
		}
	}
	if cfg.Port == 0 || cfg.Port > 65535 {
		return errors.New("postgres port must be in 1..65535")
	}
	if cfg.MaxOpenConns <= 0 || cfg.MaxOpenConns > 1024 || cfg.MaxIdleConns < 0 || cfg.MaxIdleConns > cfg.MaxOpenConns {
		return errors.New("postgres pool requires max_open_conns in 1..1024 and max_idle_conns in 0..max_open_conns")
	}
	for name, value := range map[string]*durationpb.Duration{
		"max_lifetime":      cfg.MaxLifetime,
		"max_idle_time":     cfg.MaxIdleTime,
		"connect_timeout":   cfg.ConnectTimeout,
		"statement_timeout": cfg.StatementTimeout,
	} {
		if err := value.CheckValid(); err != nil {
			return fmt.Errorf("postgres %s: %w", name, err)
		}
		if value.AsDuration() <= 0 || !proto.Equal(durationpb.New(value.AsDuration()), value) {
			return fmt.Errorf("postgres %s must be a positive Go duration", name)
		}
	}
	if timeout := cfg.StatementTimeout.AsDuration(); timeout < time.Millisecond || timeout%time.Millisecond != 0 || timeout.Milliseconds() > 1<<31-1 {
		return errors.New("postgres statement_timeout must be in 1..2147483647 milliseconds")
	}
	switch cfg.SslMode {
	case "", SSLModeVerifyFull, SSLModeRequire, SSLModeDisable:
	default:
		return errors.New("unsupported postgres ssl_mode")
	}
	if cfg.Schema != "" {
		if len(cfg.Schema) > 63 {
			return errors.New("postgres schema must be a lowercase identifier of at most 63 bytes")
		}
		for index, char := range cfg.Schema {
			if char >= 'a' && char <= 'z' || char == '_' || index > 0 && char >= '0' && char <= '9' {
				continue
			}
			return errors.New("postgres schema must be a lowercase identifier")
		}
	}
	return nil
}

func connectionConfig(cfg *configv1.Postgres, password, application string) (*pgx.ConnConfig, error) {
	mode := cfg.SslMode
	if mode == "" {
		mode = SSLModeVerifyFull
	}
	query := url.Values{}
	query.Set("sslmode", mode)
	if cfg.RootCertFile != "" {
		query.Set("sslrootcert", cfg.RootCertFile)
	}
	address := &url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(cfg.Host, strconv.FormatUint(uint64(cfg.Port), 10)),
		User:     url.UserPassword(cfg.Username, password),
		Path:     "/" + cfg.Database,
		RawPath:  "/" + url.PathEscape(cfg.Database),
		RawQuery: query.Encode(),
	}
	parsed, err := pgx.ParseConfig(address.String())
	if err != nil {
		return nil, err
	}
	parsed.ConnectTimeout = cfg.ConnectTimeout.AsDuration()
	parsed.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.AsDuration().Milliseconds(), 10)
	parsed.RuntimeParams["timezone"] = "UTC"
	parsed.RuntimeParams["application_name"] = application
	schema := cfg.Schema
	if schema == "" {
		schema = "public"
	}
	parsed.RuntimeParams["search_path"] = schema
	return parsed, nil
}
