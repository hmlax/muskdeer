package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// DefaultMarketPolicy is conservative simulation configuration, not a production SLA.
func DefaultMarketPolicy() MarketPolicy {
	return MarketPolicy{
		MaximumAge:      2 * time.Second,
		MaximumSkew:     500 * time.Millisecond,
		MaximumSpreadBP: decimal.NewFromInt(100),
		MaximumJumpBP:   decimal.NewFromInt(200),
		RecoverySamples: 3,
		RecoveryWindow:  200 * time.Millisecond,
	}
}
