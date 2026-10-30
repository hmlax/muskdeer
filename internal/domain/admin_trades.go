package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AdminTradeFilter pages authoritative fills within one explicit strategy.
// OrderID selects only the OMS intent recorded by the original submit command.
type AdminTradeFilter struct {
	StrategyID int64
	Page       AdminPage
	OrderID    int64
	Side       Side
}

// Validate requires a bounded cursor and an exact known side when provided.
func (f AdminTradeFilter) Validate() error {
	if f.StrategyID <= 0 || f.OrderID < 0 || f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize ||
		f.Side != SideUnspecified && f.Side != SideBuy && f.Side != SideSell {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminTrade is an authoritative core fill, independent of OMS confirmation and
// portfolio delivery. RecordedAt is the database recording time, not an exchange
// timestamp. OrderID is absent for an originally unlinked core submit.
type AdminTrade struct {
	ID             int64
	ExecutionVenue string
	TradeID        string
	StrategyID     int64
	AccountID      string
	InstrumentID   int64
	ConfigVersion  int64
	Source         MarketSource
	CoreOrderID    int64
	ClientOrderID  string
	OrderID        *int64
	Side           Side
	Price          decimal.Decimal
	Quantity       decimal.Decimal
	QuoteNotional  decimal.Decimal
	FeeAmount      decimal.Decimal
	FeeAsset       string
	RecordedAt     time.Time
	PortfolioFact  *AdminTradePortfolioFact
}

// AdminTradePortfolioFact means this exact fill was collected. Its presence does
// not assert that portfolio positions are complete, reconciled or safe to trade.
type AdminTradePortfolioFact struct {
	ID         int64
	RecordedAt time.Time
}

// AdminTradePage has an ascending core fill identity cursor.
type AdminTradePage struct {
	Items       []AdminTrade
	NextAfterID int64
}

// AdminTrades authenticates the current viewer in a read-only snapshot. Reads
// never apply fills, rebuild projections, confirm orders or release reservations.
type AdminTrades interface {
	List(context.Context, string, AdminTradeFilter) (AdminTradePage, error)
	Detail(context.Context, string, int64, int64) (AdminTrade, error)
}
