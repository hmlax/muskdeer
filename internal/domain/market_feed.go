package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Market feed persistence failures have safe, fixed messages.
const (
	MessageMarketFeedConflict = "market feed already has a different immutable binding"
	MessageMarketFeedMissing  = "market feed binding was not found"
	MessageMarketFeedStorage  = "market feed persistence operation failed"
)

// Feed errors do not fall back to another source or instrument.
var (
	ErrMarketFeedConflict = errors.New(MessageMarketFeedConflict)
	ErrMarketFeedMissing  = errors.New(MessageMarketFeedMissing)
)

// MarketPolicy uses explicit time units and exact decimal BPS values.
type MarketPolicy struct {
	MaximumAge      time.Duration
	MaximumSkew     time.Duration
	MaximumSpreadBP decimal.Decimal
	MaximumJumpBP   decimal.Decimal
	RecoverySamples int
	RecoveryWindow  time.Duration
}

// MarketFeedDefinition binds one execution product to an exact external product.
// ID is assigned by storage; identity, mode, version and policy are immutable.
type MarketFeedDefinition struct {
	ID            int64
	Source        MarketSource
	InstrumentID  int64
	Symbol        string
	Mode          MarketMode
	SourceVersion int64
	Policy        MarketPolicy
}

// MarketFeedRepository stores bindings only, never opens a connection or grants
// publisher authority. Create is idempotent for exactly the same typed binding.
type MarketFeedRepository interface {
	Create(ctx context.Context, feed MarketFeedDefinition) (MarketFeedDefinition, error)
	Get(ctx context.Context, id int64) (MarketFeedDefinition, error)
	Find(ctx context.Context, source MarketSource, instrumentID int64, mode MarketMode) (MarketFeedDefinition, error)
}

// Validate rejects unbounded, non-millisecond or unsafe quality policy values.
func (p MarketPolicy) Validate() error {
	if p.MaximumAge <= 0 || p.MaximumAge > 5*time.Second || p.MaximumSkew < 0 || p.MaximumSkew > time.Second ||
		p.RecoverySamples < 2 || p.RecoverySamples > 20 || p.RecoveryWindow < 0 || p.RecoveryWindow >= p.MaximumAge ||
		p.MaximumAge%time.Millisecond != 0 || p.MaximumSkew%time.Millisecond != 0 || p.RecoveryWindow%time.Millisecond != 0 ||
		!validMarketBP(p.MaximumSpreadBP) || !validMarketBP(p.MaximumJumpBP) {
		return ErrUnreliableMarket
	}
	return nil
}

// Validate enforces eligible exact source/product/mode before a database call.
func (f MarketFeedDefinition) Validate() error {
	if f.Source.ValidateSelection() != nil || f.InstrumentID <= 0 || f.SourceVersion != MarketSourceVersion ||
		(f.Mode != MarketModeLive && f.Mode != MarketModeReplay) || len(f.Symbol) == 0 || len(f.Symbol) > 64 || strings.ToUpper(f.Symbol) != f.Symbol {
		return ErrMarketIdentity
	}
	for index := range len(f.Symbol) {
		character := f.Symbol[index]
		if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || f.Source == MarketSourceOKX && character == '-' {
			continue
		}
		return ErrMarketIdentity
	}
	return f.Policy.Validate()
}

func validMarketBP(value decimal.Decimal) bool {
	return FixedDecimal(value, 5, 18, false) && value.LessThanOrEqual(decimal.NewFromInt(10000))
}
