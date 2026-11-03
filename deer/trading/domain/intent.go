package domain

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Durable event and risk-admission identifiers.
const (
	EventSchemaVersion         int64 = 1
	EventOrderIntentAccepted         = "OrderIntentAccepted"
	EventOrderStateChanged           = "OrderStateChanged"
	EventTradeFilled                 = "TradeFilled"
	EventProducerExecution           = "execution"
	OutboxStatePending               = "PENDING"
	OutboxStatePublishing            = "PUBLISHING"
	OutboxStatePublished             = "PUBLISHED"
	OutboxStateFailed                = "FAILED"
	MaxRiskSnapshotAge               = 5 * time.Second
	MessageRiskDenied                = "new risk admission is denied"
	IntentErrorOutcomeUnknown        = "OUTCOME_UNKNOWN"
	IntentErrorCoreNotFound          = "CORE_NOT_FOUND"
	IntentErrorCoreRejected          = "CORE_REJECTED"
	IntentErrorCoreNotAccepted       = "CORE_NOT_ACCEPTED"
	IntentErrorRiskDenied            = "RISK_DENIED"
)

// ErrRiskDenied covers stale/unsafe risk input and exhausted budget capacity.
var ErrRiskDenied = errors.New(MessageRiskDenied)

// IntentSnapshot retains the immutable business identity and reserved risk.
type IntentSnapshot struct {
	ID                    int64
	ClientOrderID         string
	Input                 OrderInput
	ExecutionAuthority    LeaseToken
	State                 OrderState
	CancelState           CancelState
	CoreOrderID           int64
	CoreRevision          int64
	ReservedQuoteNotional decimal.Decimal
	FilledQuantity        decimal.Decimal
	Revision              int64
	ErrorCode             string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// IntentRepository persists an intent, both budget reservations and its typed
// outbox event in one transaction. It never invokes a remote trading gateway.
type IntentRepository interface {
	Accept(ctx context.Context, input OrderInput, execution LeaseToken) (IntentSnapshot, error)
	ByID(ctx context.Context, accountID string, intentID int64) (IntentSnapshot, error)
	ByClient(ctx context.Context, accountID, clientOrderID string) (IntentSnapshot, error)
}

// IntentExecutionRepository separates durable state transitions from I/O.
type IntentExecutionRepository interface {
	IntentRepository
	Prepare(ctx context.Context, current IntentSnapshot, authority StrategyAuthority, execution LeaseToken, deadline time.Time) (IntentSnapshot, error)
	Unknown(ctx context.Context, current IntentSnapshot, code string) (IntentSnapshot, error)
	Confirm(ctx context.Context, current IntentSnapshot, order CoreOrder) (IntentSnapshot, error)
	Reject(ctx context.Context, current IntentSnapshot, code string) (IntentSnapshot, error)
}

// IntentRecovery observes a durable intent without submitting a new order.
type IntentRecovery interface {
	Recover(ctx context.Context, accountID string, intentID int64) (IntentSnapshot, error)
}

// IntentService adds submission to the observation-only recovery contract.
type IntentService interface {
	IntentRecovery
	Submit(ctx context.Context, input OrderInput, execution LeaseToken) (IntentSnapshot, error)
}
