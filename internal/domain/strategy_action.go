package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// StrategyActionKind identifies a durable outgoing operation.
type StrategyActionKind string

// StrategyActionState retains uncertainty independently of the remote order.
type StrategyActionState string

// Durable action kinds, lifecycle states and complete-observation bounds.
const (
	StrategyActionSubmit StrategyActionKind = GatewayActionSubmit
	StrategyActionCancel StrategyActionKind = GatewayActionCancel

	StrategyActionPending     StrategyActionState = "PENDING"
	StrategyActionDispatching StrategyActionState = "DISPATCHING"
	StrategyActionWaiting     StrategyActionState = "WAITING"
	StrategyActionUnknown     StrategyActionState = "UNKNOWN"
	StrategyActionObserved    StrategyActionState = "OBSERVED"
	StrategyActionDiscarded   StrategyActionState = "DISCARDED"

	MaxActiveStrategyActions = 256
)

// StrategyActionPlan is immutable business identity. A cancellation has an
// explicit target; submissions contain exact positive price and quantity.
type StrategyActionPlan struct {
	Key                 string
	Kind                StrategyActionKind
	AccountID           string
	InstrumentID        int64
	Side                Side
	Price               decimal.Decimal
	Quantity            decimal.Decimal
	TimeInForce         TimeInForce
	ReduceOnly          bool
	TargetIntentID      int64
	TargetClientOrderID string
}

// StrategyAction survives transport failure and owner replacement. Only pending
// work may be discarded; an absent record never proves an attempted call had no effect.
type StrategyAction struct {
	ID                        int64
	StrategyID                int64
	ConfigVersion             int64
	Plan                      StrategyActionPlan
	IdempotencyKey            string
	ClientOrderID             string
	State                     StrategyActionState
	Deadline                  time.Time
	IntentID                  int64
	CancellationID            int64
	ObservedOrderState        OrderState
	ObservedCancellationState CancellationState
	OwnerID                   string
	OwnerEpoch                int64
	Revision                  int64
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

// StrategyActions fences writes by current strategy lease and revision. Observe
// reads persisted OMS results by the original key; it never sends a command.
type StrategyActions interface {
	Plan(ctx context.Context, authority StrategyAuthority, plan StrategyActionPlan) (StrategyAction, error)
	Attempt(ctx context.Context, current StrategyAction, authority StrategyAuthority, deadline time.Time) (StrategyAction, error)
	Observe(ctx context.Context, current StrategyAction, authority StrategyAuthority) (StrategyAction, error)
	Discard(ctx context.Context, current StrategyAction, authority StrategyAuthority) (StrategyAction, error)
	ByKey(ctx context.Context, strategyID int64, key string) (StrategyAction, error)
	Active(ctx context.Context, strategyID int64) ([]StrategyAction, error)
}
