package runtimeconfig

import (
	"time"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
)

// ValidateSimulatorConfig requires explicit cancellation scope and bounded timing.
func ValidateSimulatorConfig(cfg *configv1.BusinessRuntime) error {
	if cfg.Simulator == nil {
		return nil
	}
	if cfg.ExecutionLeaseTtl == nil || !runtimeAccountsValid(cfg.Simulator.AccountIds) ||
		!boundedDuration(cfg.Simulator.CancelDelay, time.Millisecond, 5*time.Second) {
		return ErrInvalidConfig
	}
	return nil
}
