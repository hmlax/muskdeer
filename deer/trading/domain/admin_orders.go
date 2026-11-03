package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AdminOrderFilter pages OMS orders within one explicit strategy. The state
// filter observes OMS; independently observed core facts can be newer.
type AdminOrderFilter struct {
	StrategyID int64
	Page       AdminPage
	State      OrderState
	OpenOnly   bool
}

// Validate rejects unknown states, unbounded pages and contradictory filters.
func (f AdminOrderFilter) Validate() error {
	if f.StrategyID <= 0 || f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize ||
		f.State != OrderStateUnspecified && !ValidOrderState(f.State) ||
		f.OpenOnly && (f.State == OrderStateFilled || f.State == OrderStateCanceled || f.State == OrderStateRejected) {
		return ErrInvalidAdminQuery
	}
	return nil
}

// ValidOrderState accepts only explicit persisted lifecycle states.
func ValidOrderState(state OrderState) bool {
	switch state {
	case OrderStateIntent, OrderStateSubmitting, OrderStateLive, OrderStatePartiallyFilled, OrderStateFilled, OrderStateCanceled, OrderStateRejected, OrderStateUnknown:
		return true
	default:
		return false
	}
}

// AdminOrder exposes business identity and separate OMS/core observations.
// Submission keys and strategy/execution lease credentials are intentionally
// absent. Core presence is evidence of acceptance, never an OMS reconciliation.
// A missing core observation is not an authoritative no-effect proof.
type AdminOrder struct {
	ID                    int64
	ClientOrderID         string
	StrategyID            int64
	AccountID             string
	InstrumentID          int64
	ConfigVersion         int64
	Source                MarketSource
	Side                  Side
	Price                 decimal.Decimal
	Quantity              decimal.Decimal
	FilledQuantity        decimal.Decimal
	ReservedNotional      decimal.Decimal
	TimeInForce           TimeInForce
	ReduceOnly            bool
	State                 OrderState
	CancelState           CancelState
	ConfirmedCoreID       int64
	ConfirmedCoreRevision int64
	Revision              int64
	ErrorCode             string
	Deadline              time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Core                  *CoreOrder
	CoreUpdatedAt         time.Time
}

// AdminOrderPage has a bounded ascending OMS identity cursor.
type AdminOrderPage struct {
	Items       []AdminOrder
	NextAfterID int64
}

// AdminOrders authenticates the current viewer inside every read-only snapshot.
// Reads never reconcile OMS, cancel orders, release reservations or grant risk.
type AdminOrders interface {
	List(context.Context, string, AdminOrderFilter) (AdminOrderPage, error)
	Detail(context.Context, string, int64, int64) (AdminOrder, error)
}
