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

// Instrument metadata and simulated execution identifiers.
const (
	InstrumentStatusTrading = "TRADING"
	InstrumentStatusHalted  = "HALTED"
	ExecutionVenueSimulator = "simulator"
	MaxStrategyOpenOrders   = 200
)
