package runtimeconfig

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestNativeRuntimeRequiresExplicitTransportAndFiniteControls(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:            "execution",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Postgres:        &configv1.Postgres{},
		Grpc:            &configv1.Listener{},
		GrpcSecurity:    &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}},
		GrpcClients:     &configv1.GRPCClients{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   native.Venue,
			PollInterval:     durationpb.New(200 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
			Himalaya: &configv1.HimalayaRuntime{
				Target:           "127.0.0.1:8080",
				TokenEnv:         "TEST_NATIVE_TOKEN",
				QuantityFraction: "0.1",
				Depth:            20,
				LeaseTtl:         durationpb.New(10 * time.Second),
			},
		},
	}
	for _, role := range []string{"market", "strategy", "execution", "portfolio"} {
		candidate := proto.CloneOf(cfg)
		candidate.Name = role
		if err := ValidateConfig(candidate); err != nil {
			t.Fatalf("valid %s runtime rejected: %v", role, err)
		}
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(c *configv1.ServiceConfig) { c.Business.Himalaya.Target = "public.example:8080" },
		func(c *configv1.ServiceConfig) { c.Business.Himalaya.QuantityFraction = "0" },
		func(c *configv1.ServiceConfig) { c.Business.Himalaya.Depth = 0 },
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if ValidateConfig(changed) == nil {
			t.Fatal("unsafe native startup was accepted")
		}
	}
}
