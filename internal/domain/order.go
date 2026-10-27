package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// TimeInForce identifies explicit limit-order execution policies.
type TimeInForce string

// Supported time-in-force identifiers; the zero value is not a default.
const (
	TimeInForceUnspecified TimeInForce = ""
	TimeInForceGTC         TimeInForce = "GTC"
	TimeInForceIOC         TimeInForce = "IOC"
	TimeInForcePostOnly    TimeInForce = "POST_ONLY"
)

// OrderState preserves uncertain submissions and fill progress independently.
type OrderState string

// Order lifecycle identifiers.
const (
	OrderStateIntent          OrderState = "INTENT"
	OrderStateSubmitting      OrderState = "SUBMITTING"
	OrderStateLive            OrderState = "LIVE"
	OrderStatePartiallyFilled OrderState = "PARTIALLY_FILLED"
	OrderStateFilled          OrderState = "FILLED"
	OrderStateCanceled        OrderState = "CANCELED"
	OrderStateRejected        OrderState = "REJECTED"
	OrderStateUnknown         OrderState = "UNKNOWN"
)

// CancelState is separate from order state so cancellation cannot erase fills.
type CancelState string

// Cancellation lifecycle identifiers.
const (
	CancelStateNone      CancelState = "NONE"
	CancelStateRequested CancelState = "REQUESTED"
	CancelStatePending   CancelState = "PENDING"
	CancelStateConfirmed CancelState = "CONFIRMED"
	CancelStateUnknown   CancelState = "UNKNOWN"
	CancelStateRejected  CancelState = "REJECTED"
)

// Trading boundary limits and stable public messages.
const (
	MaxIntentLifetime   = time.Minute
	MessageInvalidOrder = "invalid trading request"
)

// ErrInvalidOrder identifies a field-level trading contract violation.
var ErrInvalidOrder = errors.New(MessageInvalidOrder)

// OrderValidationError omits the supplied value from public error text.
type OrderValidationError struct{ Field string }

func (e *OrderValidationError) Error() string { return MessageInvalidOrder + ": " + e.Field }
func (*OrderValidationError) Unwrap() error   { return ErrInvalidOrder }

// StrategyAuthority identifies a proposed owner, not verified authorization.
// The authoritative lease and final gateway must verify it before effects.
type StrategyAuthority struct {
	StrategyID    int64
	ConfigVersion int64
	OwnerID       string
	OwnerEpoch    int64
}

// OrderInput holds parsed intent data; validation does not reserve any funds.
type OrderInput struct {
	RequestID      string
	IdempotencyKey string
	Authority      StrategyAuthority
	AccountID      string
	InstrumentID   int64
	Side           Side
	Price          decimal.Decimal
	Quantity       decimal.Decimal
	TimeInForce    TimeInForce
	ReduceOnly     bool
	Deadline       time.Time
}

// CancelInput identifies a cancellation without changing the original order.
type CancelInput struct {
	RequestID      string
	IdempotencyKey string
	Authority      StrategyAuthority
	AccountID      string
	InstrumentID   int64
	ClientOrderID  string
	Deadline       time.Time
}
