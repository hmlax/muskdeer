package runtimeconfig

import (
	"slices"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func validateStrategyConfig(cfg *configv1.ServiceConfig) error {
	strategy := cfg.Business.Strategy
	if strategy == nil ||
		cfg.Business.PortfolioConsumer != "" || cfg.Business.Market != nil || cfg.Business.Risk != nil || cfg.Business.ExecutionLeaseTtl != nil || cfg.Business.Simulator != nil {
		return ErrInvalidConfig
	}
	if strategy.ManagementEnabled && !slices.Contains(cfg.GrpcSecurity.AllowedPeerServices, rpcsecurity.ServiceAPI) {
		return ErrInvalidConfig
	}
	if strategy.ControlEnabled && !strategy.ManagementEnabled {
		return ErrInvalidConfig
	}
	if len(strategy.AccountIds) == 0 {
		if !strategy.ManagementEnabled || strategy.LeaseTtl != nil || cfg.GrpcClients != nil {
			return ErrInvalidConfig
		}
		return nil
	}
	if !runtimeAccountsValid(strategy.AccountIds) || cfg.GrpcClients == nil ||
		!boundedDuration(strategy.LeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || strategy.LeaseTtl.AsDuration() <= 2*cfg.Business.OperationTimeout.AsDuration() ||
		!slices.Contains(cfg.GrpcClients.AllowedServices, rpcsecurity.ServiceMarket) || !slices.Contains(cfg.GrpcClients.AllowedServices, rpcsecurity.ServiceExecution) {
		return ErrInvalidConfig
	}
	return nil
}
