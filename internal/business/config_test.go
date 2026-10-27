package business

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestBusinessRequiresExplicitModeAndAllOwnedDependencies(t *testing.T) {
	if err := ValidateConfig(&configv1.ServiceConfig{Name: "execution"}); err != nil {
		t.Fatalf("infrastructure-only configuration changed: %v", err)
	}
	fixture := &configv1.ServiceConfig{
		Name:            "execution",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		GrpcSecurity:    &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}},
		Postgres:        &configv1.Postgres{},
		Jetstream:       &configv1.JetStream{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(100 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
		},
	}
	if err := ValidateConfig(fixture); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(c *configv1.ServiceConfig) { c.Name = "strategy" },
		func(c *configv1.ServiceConfig) { c.Business.ExecutionVenue = "real" },
		func(c *configv1.ServiceConfig) { c.Postgres = nil },
		func(c *configv1.ServiceConfig) { c.Jetstream = nil },
		func(c *configv1.ServiceConfig) { c.Nacos.Registration = nil },
		func(c *configv1.ServiceConfig) { c.GrpcSecurity = nil },
		func(c *configv1.ServiceConfig) { c.Business.PollInterval = durationpb.New(time.Nanosecond) },
		func(c *configv1.ServiceConfig) { c.Business.OperationTimeout = durationpb.New(6 * time.Second) },
		func(c *configv1.ServiceConfig) { c.Name = "portfolio" },
	} {
		changed := proto.CloneOf(fixture)
		mutate(changed)
		if err := ValidateConfig(changed); err == nil {
			t.Fatal("incomplete/unsupported business mode accepted")
		}
	}
}
