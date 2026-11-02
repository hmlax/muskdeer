package business

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestOperatorValidatesEveryEnabledModuleAndOwnsOneIdentity(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:            "operator",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		Postgres:        &configv1.Postgres{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Jetstream:       &configv1.JetStream{},
		GrpcSecurity: &configv1.GRPCServerSecurity{
			Mtls:                &configv1.MutualTLS{},
			AllowedPeerServices: []string{rpcsecurity.ServiceAdmin, rpcsecurity.ServiceOperator},
		},
		GrpcClients: &configv1.GRPCClients{AllowedServices: []string{rpcsecurity.ServiceOperator}},
		Business: &configv1.BusinessRuntime{
			Enabled:           true,
			ExecutionVenue:    domain.ExecutionVenueSimulator,
			PollInterval:      durationpb.New(100 * time.Millisecond),
			OperationTimeout:  durationpb.New(time.Second),
			ExecutionLeaseTtl: durationpb.New(3 * time.Second),
			PortfolioConsumer: "portfolio_operator",
			Strategy: &configv1.StrategyRuntime{
				ManagementEnabled: true,
				ControlEnabled:    true,
				AccountIds:        []string{"account-one"},
				LeaseTtl:          durationpb.New(3 * time.Second),
			},
			Market: &configv1.MarketRuntime{
				Feeds:    []*configv1.MarketFeedRuntime{{FeedId: 1}},
				LeaseTtl: durationpb.New(3 * time.Second),
			},
			Risk: &configv1.RiskRuntime{AccountIds: []string{"account-one"}},
		},
	}
	before := proto.CloneOf(cfg)
	if err := ValidateConfig(cfg); err != nil || !proto.Equal(cfg, before) {
		t.Fatalf("operator module validation failed or mutated input: %v", err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(c *configv1.ServiceConfig) { c.GrpcSecurity.AllowedPeerServices = nil },
		func(c *configv1.ServiceConfig) { c.GrpcClients.AllowedServices = nil },
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds = nil },
		func(c *configv1.ServiceConfig) { c.Business.Strategy.LeaseTtl = durationpb.New(time.Second) },
		func(c *configv1.ServiceConfig) { c.Business.PortfolioConsumer = "invalid consumer" },
		func(c *configv1.ServiceConfig) { c.Jetstream = nil },
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if ValidateConfig(changed) == nil {
			t.Fatal("operator admitted an invalid module dependency")
		}
	}
}
