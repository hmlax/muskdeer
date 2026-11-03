package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// MarketReplayConfig contains explicit simulator inputs, never a live-source fallback.
type MarketReplayConfig struct {
	Bid             decimal.Decimal
	Ask             decimal.Decimal
	Quantity        decimal.Decimal
	Interval        time.Duration
	CallbackTimeout time.Duration
}
