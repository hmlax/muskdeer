package runtimeconfig

import (
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func validateRiskConfig(cfg *configv1.BusinessRuntime) error {
	if cfg.ExecutionLeaseTtl != nil && (!boundedDuration(cfg.ExecutionLeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || cfg.ExecutionLeaseTtl.AsDuration() <= 2*cfg.OperationTimeout.AsDuration()) {
		return ErrInvalidConfig
	}
	if cfg.Risk == nil {
		return nil
	}
	if cfg.ExecutionLeaseTtl == nil || !runtimeAccountsValid(cfg.Risk.AccountIds) {
		return ErrInvalidConfig
	}
	return nil
}
