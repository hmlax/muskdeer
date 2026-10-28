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

func TestStrategyRuntimeNeedsExplicitAccountsLeaseAndBothReaders(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:            "strategy",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		GrpcSecurity:    &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}},
		Postgres:        &configv1.Postgres{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		GrpcClients: &configv1.GRPCClients{
			AllowedServices: []string{rpcsecurity.ServiceMarket, rpcsecurity.ServiceExecution},
		},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(100 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
			Strategy: &configv1.StrategyRuntime{
				AccountIds: []string{"strategy-account"},
				LeaseTtl:   durationpb.New(3 * time.Second),
			},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(v *configv1.ServiceConfig) { v.Business.Strategy = nil },
		func(v *configv1.ServiceConfig) { v.Business.Strategy.AccountIds = nil },
		func(v *configv1.ServiceConfig) { v.Business.Strategy.AccountIds = []string{"same", "same"} },
		func(v *configv1.ServiceConfig) { v.Business.Strategy.AccountIds = []string{"bad/account"} },
		func(v *configv1.ServiceConfig) { v.Business.Strategy.LeaseTtl = durationpb.New(2 * time.Second) },
		func(v *configv1.ServiceConfig) { v.GrpcClients = nil },
		func(v *configv1.ServiceConfig) { v.GrpcClients.AllowedServices = []string{rpcsecurity.ServiceMarket} },
		func(v *configv1.ServiceConfig) { v.Business.Risk = &configv1.RiskRuntime{} },
		func(v *configv1.ServiceConfig) { v.Business.Simulator = &configv1.SimulatorScenarioRuntime{} },
		func(v *configv1.ServiceConfig) {
			v.Name = "execution"
			v.Jetstream = &configv1.JetStream{}
		},
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if err := ValidateConfig(changed); err == nil {
			t.Fatal("incomplete or misplaced strategy runtime accepted")
		}
	}
}

func TestManagementOnlyStrategyRequiresExplicitOptInWithoutTradingBindings(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:            "strategy",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		GrpcSecurity: &configv1.GRPCServerSecurity{
			Mtls:                &configv1.MutualTLS{},
			AllowedPeerServices: []string{rpcsecurity.ServiceAPI},
		},
		Postgres: &configv1.Postgres{},
		Nacos:    &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(100 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
			Strategy:         &configv1.StrategyRuntime{ManagementEnabled: true},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(v *configv1.ServiceConfig) { v.Business.Strategy.ManagementEnabled = false },
		func(v *configv1.ServiceConfig) { v.Business.Strategy.LeaseTtl = durationpb.New(3 * time.Second) },
		func(v *configv1.ServiceConfig) { v.GrpcClients = &configv1.GRPCClients{} },
		func(v *configv1.ServiceConfig) {
			v.Business.Strategy.AccountIds = []string{"not-authorized-for-trading"}
		},
		func(v *configv1.ServiceConfig) { v.Postgres = nil },
		func(v *configv1.ServiceConfig) { v.GrpcSecurity = nil },
		func(v *configv1.ServiceConfig) { v.Nacos = nil },
		func(v *configv1.ServiceConfig) {
			v.GrpcSecurity.AllowedPeerServices = []string{rpcsecurity.ServicePortfolio}
		},
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if err := ValidateConfig(changed); err == nil {
			t.Fatal("management-only opt-in accepted incomplete or trading bindings")
		}
	}
}
