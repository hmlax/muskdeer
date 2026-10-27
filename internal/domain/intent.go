package domain

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Durable event and risk-admission identifiers.
const (
	EventSchemaVersion       int64 = 1
	EventOrderIntentAccepted       = "OrderIntentAccepted"
	EventOrderStateChanged         = "OrderStateChanged"
	EventTradeFilled               = "TradeFilled"
	EventProducerExecution         = "execution"
	OutboxStatePending             = "PENDING"
	OutboxStatePublishing          = "PUBLISHING"
	OutboxStatePublished           = "PUBLISHED"
	OutboxStateFailed              = "FAILED"
	MaxRiskSnapshotAge             = 5 * time.Second
	MessageRiskDenied              = "new risk admission is denied"
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
	ReservedQuoteNotional decimal.Decimal
	FilledQuantity        decimal.Decimal
	Revision              int64
	ErrorCode             string
}

// IntentRepository persists an intent, both budget reservations and its typed
// outbox event in one transaction. It never invokes a remote trading gateway.
type IntentRepository interface {
	Accept(ctx context.Context, input OrderInput, execution LeaseToken) (IntentSnapshot, error)
	ByID(ctx context.Context, accountID string, intentID int64) (IntentSnapshot, error)
	ByClient(ctx context.Context, accountID, clientOrderID string) (IntentSnapshot, error)
}
