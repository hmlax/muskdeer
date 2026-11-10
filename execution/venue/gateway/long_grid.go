package gateway

import (
	"context"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/shopspring/decimal"
)

// GridRequest is one journaled long-grid command. Sells must reduce an exact position.
type GridRequest struct {
	Scope      Scope
	ClientID   string
	Side       domain.Side
	Price      decimal.Decimal
	Quantity   decimal.Decimal
	PositionID string
	Leverage   uint32
	ReduceOnly bool
	Market     bool
}

// GridOrder contains positive terminal venue facts, including partial fills.
type GridOrder struct {
	OrderID          uint64
	ClientID         string
	Side             domain.Side
	ReduceOnly       bool
	Quantity         string
	ExecutedQuantity string
	ExecutedNotional string
	Status           string
}

// LongGridClient supports long-only hedge-account observations and closing legs.
// The owner journals the command before SubmitGrid; the adapter sends once.
type LongGridClient interface {
	ReadGrid(context.Context, Scope) (*Observation, error)
	SubmitGrid(context.Context, GridRequest) (*SubmitResult, error)
	FindGridOrder(context.Context, Scope, string, uint64, int32) (*GridOrder, error)
}
