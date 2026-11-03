package domain

// StrategyTemplate identifies the two initial, explicitly configured algorithms.
type StrategyTemplate string

// Supported strategy template identifiers.
const (
	StrategyTemplateMarketMaker StrategyTemplate = "market_maker"
	StrategyTemplateGrid        StrategyTemplate = "grid"
)

// StrategyState is persisted independently of the user's desired running flag.
type StrategyState string

// Strategy lifecycle state identifiers.
const (
	StrategyStateCreated    StrategyState = "CREATED"
	StrategyStateStarting   StrategyState = "STARTING"
	StrategyStateRecovering StrategyState = "RECOVERING"
	StrategyStateRunning    StrategyState = "RUNNING"
	StrategyStatePausing    StrategyState = "PAUSING"
	StrategyStatePaused     StrategyState = "PAUSED"
	StrategyStateStopping   StrategyState = "STOPPING"
	StrategyStateStopped    StrategyState = "STOPPED"
	StrategyStateError      StrategyState = "ERROR"
)

// ValidStrategyState accepts stored lifecycle identifiers, not execution grants.
func ValidStrategyState(state StrategyState) bool {
	switch state {
	case StrategyStateCreated, StrategyStateStarting, StrategyStateRecovering, StrategyStateRunning,
		StrategyStatePausing, StrategyStatePaused, StrategyStateStopping, StrategyStateStopped, StrategyStateError:
		return true
	default:
		return false
	}
}

// Instrument metadata and simulated execution identifiers.
const (
	InstrumentStatusTrading = "TRADING"
	InstrumentStatusHalted  = "HALTED"
	ExecutionVenueSimulator = "simulator"
	MaxStrategyOpenOrders   = 200
)
