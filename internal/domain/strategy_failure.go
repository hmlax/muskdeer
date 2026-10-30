package domain

// StrategyStage is a fixed diagnostic identifier, never request content.
type StrategyStage string

// Strategy operation stages identify the boundary that failed.
const (
	StrategyStageBoundary         StrategyStage = "strategy.boundary"
	StrategyStageAcquire          StrategyStage = "strategy.acquire"
	StrategyStageRenew            StrategyStage = "strategy.renew"
	StrategyStageInstance         StrategyStage = "strategy.instance"
	StrategyStageClaim            StrategyStage = "strategy.claim"
	StrategyStageActions          StrategyStage = "strategy.actions"
	StrategyStageManagementCancel StrategyStage = "strategy.management_cancel"
	StrategyStageOrders           StrategyStage = "strategy.orders"
	StrategyStageStop             StrategyStage = "strategy.stop"
	StrategyStageRecover          StrategyStage = "strategy.recover"
	StrategyStageQuote            StrategyStage = "strategy.quote"
	MessageStrategyFailed                       = "strategy operation failed"
)

// StrategyFailure preserves private causes while exposing a fixed stage.
type StrategyFailure struct {
	Stage StrategyStage
	Cause error
}

func (*StrategyFailure) Error() string   { return MessageStrategyFailed }
func (e *StrategyFailure) Unwrap() error { return e.Cause }

// SafeStage only returns declared diagnostic identifiers.
func (e *StrategyFailure) SafeStage() string {
	switch e.Stage {
	case StrategyStageBoundary, StrategyStageAcquire, StrategyStageRenew, StrategyStageInstance, StrategyStageClaim, StrategyStageActions, StrategyStageManagementCancel, StrategyStageOrders, StrategyStageStop, StrategyStageRecover, StrategyStageQuote:
		return string(e.Stage)
	default:
		return ""
	}
}
