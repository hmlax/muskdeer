package postgres

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestCanceledConstructionDoesNotConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client, err := New(ctx, testConfig(), "canceled")
	if client != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled New = %v, %v", client, err)
	}
}

func TestInvalidPoolConfigurationIsRejected(t *testing.T) {
	tests := []struct {
		name   string
		change func(*configv1.Postgres)
	}{
		{
			name:   "unlimited pool",
			change: func(cfg *configv1.Postgres) { cfg.MaxOpenConns = 0 },
		},
		{
			name:   "idle exceeds open",
			change: func(cfg *configv1.Postgres) { cfg.MaxIdleConns = 3 },
		},
		{
			name:   "missing connect budget",
			change: func(cfg *configv1.Postgres) { cfg.ConnectTimeout = nil },
		},
		{
			name:   "submillisecond server timeout",
			change: func(cfg *configv1.Postgres) { cfg.StatementTimeout = durationpb.New(time.Nanosecond) },
		},
		{
			name:   "unknown TLS mode",
			change: func(cfg *configv1.Postgres) { cfg.SslMode = "unexpected" },
		},
		{
			name:   "fractional millisecond timeout",
			change: func(cfg *configv1.Postgres) { cfg.StatementTimeout = durationpb.New(1500 * time.Microsecond) },
		},
		{
			name:   "invalid schema",
			change: func(cfg *configv1.Postgres) { cfg.Schema = "public,untrusted" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			test.change(cfg)
			if client, err := New(t.Context(), cfg, "invalid"); client != nil || err == nil {
				t.Fatalf("invalid New = %v, %v", client, err)
			}
		})
	}
}

func TestMissingSecretIsRejected(t *testing.T) {
	cfg := testConfig()
	t.Setenv(cfg.PasswordEnv, "")
	if client, err := New(t.Context(), cfg, "no-secret"); client != nil || err == nil {
		t.Fatalf("empty secret New = %v, %v", client, err)
	}
}

func TestConnectionFailureHidesPasswordAndPreservesCause(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Host = host
	cfg.Port = uint32(number)
	const secret = "private-test-only-password"
	t.Setenv(cfg.PasswordEnv, secret)
	client, err := New(t.Context(), cfg, "unavailable")
	if client != nil || err == nil {
		t.Fatalf("unavailable New = %v, %v", client, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("password leaked in startup error: %v", err)
	}
	var networkError *net.OpError
	if !errors.As(err, &networkError) {
		t.Fatalf("startup error lost its network cause: %v", err)
	}
}

func testConfig() *configv1.Postgres {
	return &configv1.Postgres{
		Host:             "127.0.0.1",
		Port:             5432,
		Database:         "muskdeer_test_only",
		Username:         "muskdeer_test_only",
		PasswordEnv:      "MUSKDEER_UNIT_TEST_POSTGRES_PASSWORD",
		SslMode:          "disable",
		MaxOpenConns:     2,
		MaxIdleConns:     1,
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(30 * time.Second),
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(time.Second),
	}
}
