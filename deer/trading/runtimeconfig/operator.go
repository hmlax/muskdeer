package runtimeconfig

import (
	"slices"

	"google.golang.org/protobuf/proto"

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
		validateRiskConfig(cfg.Business) != nil || ValidateSimulatorConfig(cfg.Business) != nil {
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
