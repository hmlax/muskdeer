package deer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestDeerRejectsInvalidConfigBeforeSideEffects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*configv1.ServiceConfig) *configv1.ServiceConfig
	}{
		{
			name:   "nil config",
			change: func(*configv1.ServiceConfig) *configv1.ServiceConfig { return nil },
		},
		{
			name: "public unauthenticated monitoring",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Monitoring = &configv1.Listener{
					Address:        "0.0.0.0:0",
					RequestTimeout: durationpb.New(time.Second),
				}
				return cfg
			},
		},
		{
			name: "outbound RPC requires discovery",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.GrpcClients = &configv1.GRPCClients{}
				return cfg
			},
		},
		{
			name: "public plaintext gRPC",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Grpc.Address = "0.0.0.0:0"
				return cfg
			},
		},
		{
			name: "security without gRPC listener",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Grpc = nil
				cfg.GrpcSecurity = &configv1.GRPCServerSecurity{}
				return cfg
			},
		},
		{
			name: "missing identity",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.InstanceId = ""
				return cfg
			},
		},
		{
			name: "missing startup budget",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.StartupTimeout = nil
				return cfg
			},
		},
		{
			name: "zero shutdown budget",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.ShutdownTimeout = durationpb.New(0)
				return cfg
			},
		},
		{
			name: "empty listener address",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Grpc.Address = ""
				return cfg
			},
		},
		{
			name: "request outlives drain budget",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Grpc.RequestTimeout = durationpb.New(4 * time.Second)
				return cfg
			},
		},
		{
			name: "HTTP requires API wrapper",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Http = cfg.Grpc
				return cfg
			},
		},
		{
			name: "publication requires gRPC listener",
			change: func(cfg *configv1.ServiceConfig) *configv1.ServiceConfig {
				cfg.Grpc = nil
				cfg.Nacos = &configv1.Nacos{
					Registration: &configv1.NacosRegistration{},
				}
				return cfg
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var opened atomic.Int64
			_, _, err := NewDeer(t.Context(), test.change(serviceConfig("invalid")), ServiceSpec{
				Resources: []Initializer{
					{
						Name: "must not open",
						Open: func(context.Context) (Cleanup, error) {
							opened.Add(1)
							return func(context.Context) error { return nil }, nil
						},
					},
				},
			})
			if err == nil || opened.Load() != 0 {
				t.Fatalf("invalid config = %v, opened %d; want rejection without side effects", err, opened.Load())
			}
		})
	}
}
