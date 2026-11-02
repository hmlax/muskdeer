package business

import (
	"context"
	"slices"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

// validateOperatorConfig reuses each module's rules without creating another
// Deer, connection pool, listener or service registration.
func validateOperatorConfig(cfg *configv1.ServiceConfig) error {
	if !slices.Contains(cfg.GrpcSecurity.AllowedPeerServices, rpcsecurity.ServiceAdmin) ||
		cfg.Business.Strategy == nil || !cfg.Business.Strategy.ManagementEnabled {
		return ErrInvalidConfig
	}
	if cfg.Business.Market != nil && validateMarketConfig(cfg.Business) != nil ||
		validateRiskConfig(cfg.Business) != nil || validateSimulatorConfig(cfg.Business) != nil {
		return ErrInvalidConfig
	}
	if cfg.Business.PortfolioConsumer != "" && (!consumerName(cfg.Business.PortfolioConsumer) || cfg.Jetstream == nil || cfg.GrpcClients == nil) {
		return ErrInvalidConfig
	}
	strategy := proto.CloneOf(cfg)
	strategy.Business.Market = nil
	strategy.Business.Risk = nil
	strategy.Business.Simulator = nil
	strategy.Business.ExecutionLeaseTtl = nil
	strategy.Business.PortfolioConsumer = ""
	strategy.GrpcSecurity.AllowedPeerServices = append(strategy.GrpcSecurity.AllowedPeerServices, rpcsecurity.ServiceAPI)
	if len(strategy.Business.Strategy.AccountIds) != 0 {
		if cfg.Jetstream == nil || cfg.GrpcClients == nil || !slices.Contains(cfg.GrpcSecurity.AllowedPeerServices, rpcsecurity.ServiceOperator) || !slices.Contains(cfg.GrpcClients.AllowedServices, rpcsecurity.ServiceOperator) {
			return ErrInvalidConfig
		}
		strategy.GrpcClients.AllowedServices = append(strategy.GrpcClients.AllowedServices, rpcsecurity.ServiceMarket, rpcsecurity.ServiceExecution)
	}
	return validateStrategyConfig(strategy)
}

func newOperator(ctx context.Context, db *gorm.DB, owner *deer.Deer, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	runtime := &Runtime{}
	add := func(module *Runtime) {
		runtime.modules = append(runtime.modules, module)
		runtime.workers = append(runtime.workers, module.Workers()...)
	}
	if cfg.Market != nil {
		module, err := newMarket(ctx, db, incarnation, cfg)
		if err != nil {
			return nil, err
		}
		add(module)
	}
	if ownerConfigNeedsEvents(cfg) {
		events, err := owner.JetStream()
		if err != nil {
			return nil, err
		}
		module, err := newExecution(ctx, db, events, incarnation, cfg)
		if err != nil {
			return nil, err
		}
		add(module)
		if cfg.PortfolioConsumer != "" {
			module, err = newPortfolio(ctx, db, owner, events, cfg)
			if err != nil {
				return nil, err
			}
			add(module)
		}
	}
	module, err := newStrategy(ctx, db, owner, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	module.trustedAdmin = true
	add(module)
	return runtime, nil
}

func ownerConfigNeedsEvents(cfg *configv1.BusinessRuntime) bool {
	return len(cfg.Strategy.AccountIds) != 0 || cfg.Risk != nil || cfg.Simulator != nil || cfg.PortfolioConsumer != ""
}
