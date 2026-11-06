package runtimeconfig

import (
	"time"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// ValidatePolicyParameters validates database values without requiring transport
// resources. Startup additionally validates the actual service dependencies.
func ValidatePolicyParameters(service string, parameters *configv1.BusinessRuntime) error {
	if !policyService(service) || parameters == nil || parameters.GetStrategy().GetManagementEnabled() || parameters.GetStrategy().GetControlEnabled() {
		return ErrInvalidConfig
	}
	mode := parameters.ExecutionVenue
	if mode != "" && mode != native.Venue && mode != domain.ExecutionVenueSimulator {
		return ErrInvalidConfig
	}
	if !parameters.Enabled {
		return nil
	}
	if !boundedDuration(parameters.PollInterval, 10*time.Millisecond, 5*time.Second) || !boundedDuration(parameters.OperationTimeout, 100*time.Millisecond, 5*time.Second) {
		return ErrInvalidConfig
	}
	if mode == native.Venue {
		return native.ValidateParameters(parameters)
	}
	if mode != domain.ExecutionVenueSimulator || parameters.Venue != nil {
		return ErrInvalidConfig
	}
	return validateSimulatorPolicy(service, parameters)
}

func validateSimulatorPolicy(service string, parameters *configv1.BusinessRuntime) error {
	switch service + ".grpc" {
	case rpcsecurity.ServiceExecution:
		if parameters.PortfolioConsumer != "" || parameters.Market != nil || parameters.Strategy != nil || validateRiskConfig(parameters) != nil || ValidateSimulatorConfig(parameters) != nil {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServicePortfolio:
		if !consumerName(parameters.PortfolioConsumer) || parameters.Market != nil || parameters.Risk != nil || parameters.ExecutionLeaseTtl != nil || parameters.Simulator != nil || parameters.Strategy != nil {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServiceMarket:
		if parameters.PortfolioConsumer != "" || validateMarketConfig(parameters) != nil || parameters.Risk != nil || parameters.ExecutionLeaseTtl != nil || parameters.Simulator != nil || parameters.Strategy != nil {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServiceStrategy:
		return validateStrategyParameters(parameters)
	default:
		return ErrInvalidConfig
	}
	return nil
}

func validateStrategyParameters(parameters *configv1.BusinessRuntime) error {
	strategy := parameters.Strategy
	if strategy == nil || parameters.PortfolioConsumer != "" || parameters.Market != nil || parameters.Risk != nil || parameters.ExecutionLeaseTtl != nil || parameters.Simulator != nil || !runtimeAccountsValid(strategy.AccountIds) || !boundedDuration(strategy.LeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || strategy.LeaseTtl.AsDuration() <= 2*parameters.OperationTimeout.AsDuration() {
		return ErrInvalidConfig
	}
	return nil
}
