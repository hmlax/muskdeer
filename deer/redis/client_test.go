package redis

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestCanceledConstructorDoesNotConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client, err := New(ctx, testConfig(), "canceled")
	if client != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled New = %v, %v", client, err)
	}
}

func TestInvalidConfigurationIsRejectedBeforeIO(t *testing.T) {
	tests := []struct {
		name   string
		change func(*configv1.Redis)
	}{
		{
			name:   "unbounded pool",
			change: func(cfg *configv1.Redis) { cfg.PoolSize = 0 },
		},
		{
			name:   "missing namespace",
			change: func(cfg *configv1.Redis) { cfg.KeyPrefix = "" },
		},
		{
			name:   "namespace lacks service",
			change: func(cfg *configv1.Redis) { cfg.KeyPrefix = "production" },
		},
		{
			name:   "credential URL instead of address",
			change: func(cfg *configv1.Redis) { cfg.Address = "redis://user:secret@127.0.0.1:6379" },
		},
		{
			name:   "missing command timeout",
			change: func(cfg *configv1.Redis) { cfg.CommandTimeout = nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			test.change(cfg)
			if err := validate(cfg); err == nil {
				t.Fatal("invalid Redis settings were accepted")
			}
		})
	}
}

func TestConnectionFailureClosesClientResources(t *testing.T) {
	synctest.Test(t, testConnectionFailure)
}

func TestTLSDefaultsAndExplicitDisable(t *testing.T) {
	cfg := testConfig()
	cfg.TlsEnabled = nil
	secure, err := connectionTLS(cfg)
	if err != nil || secure == nil || secure.InsecureSkipVerify || secure.ServerName != "127.0.0.1" {
		t.Fatalf("default TLS = %+v, %v", secure, err)
	}
	cfg.TlsEnabled = proto.Bool(false)
	cfg.RootCertFile = "unexpected.pem"
	if _, err := connectionTLS(cfg); err == nil {
		t.Fatal("root certificate accepted with disabled TLS")
	}
}

func testConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Address = address
	cfg.PoolSize = 1
	client, err := New(t.Context(), cfg, "connection-failure")
	if client != nil || err == nil {
		t.Fatalf("unavailable New = %v, %v", client, err)
	}
	var networkError *net.OpError
	if !errors.As(err, &networkError) {
		t.Fatalf("network failure lost its cause: %v", err)
	}
	// The SDK's failed-dial recovery probe checks closure after a one-second
	// timer. Keep the bubble alive for its documented dial+timer bound; virtual
	// time advances immediately. synctest then requires every child to exit.
	settled, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	<-settled.Done()
}

func testConfig() *configv1.Redis {
	return &configv1.Redis{
		Address:        "127.0.0.1:6379",
		KeyPrefix:      "test:cache",
		PoolSize:       2,
		ConnectTimeout: durationpb.New(time.Second),
		CommandTimeout: durationpb.New(time.Second),
		PoolTimeout:    durationpb.New(100 * time.Millisecond),
		MaxIdleTime:    durationpb.New(30 * time.Second),
		MaxLifetime:    durationpb.New(time.Minute),
		TlsEnabled:     proto.Bool(false),
	}
}
