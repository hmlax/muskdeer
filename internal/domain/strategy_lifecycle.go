package domain

import "context"

// StrategySpec separates requested input, active input and the current runtime
// incarnation. It is metadata, not a permission to submit an order.
type StrategySpec struct {
	ID                int64
	Key               string
	AccountID         string
	InstrumentID      int64
	Template          StrategyTemplate
	CodeVersion       string
	Revision          int64
	State             StrategyState
	DesiredRunning    bool
	RequestedVersion  int64
	RequestedSource   MarketSource
	ActiveVersion     int64
	ActiveSource      MarketSource
	Mode              MarketMode
	RuntimeOwnerID    string
	RuntimeOwnerEpoch int64
}

// StrategyTransition changes actual lifecycle state under a current lease.
// Requested configuration only activates after old obligations have converged.
type StrategyTransition struct {
	State             StrategyState
	ActivateRequested bool
}

// StrategyLifecycle persists recovery, activation and completion. It never
// creates balances/budgets, grants risk permission or claims process exit stopped orders.
type StrategyLifecycle interface {
	Instance(ctx context.Context, strategyID int64) (StrategySpec, error)
	Transition(ctx context.Context, current StrategySpec, token LeaseToken, transition StrategyTransition) (StrategySpec, error)
}
