package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Parameter limits bound computation and duration conversion, not service SLAs.
const (
	ParametersSchemaVersion  int64 = 1
	MaxQuoteLevels                 = 20
	MaxGridCount                   = 200
	MaxStrategyInterval            = time.Hour
	MessageInvalidParameters       = "invalid strategy parameters"
)

// ErrInvalidParameters identifies field or cross-field configuration failures.
var ErrInvalidParameters = errors.New(MessageInvalidParameters)

// ParameterError identifies a field without echoing its untrusted input value.
type ParameterError struct{ Field string }

func (e *ParameterError) Error() string { return MessageInvalidParameters + ": " + e.Field }
func (*ParameterError) Unwrap() error   { return ErrInvalidParameters }

// MakerParameters uses quote-currency order budgets and base-asset inventory.
type MakerParameters struct {
	QuoteLevels        int32
	LevelSpacingBPS    decimal.Decimal
	BaseSpreadBPS      decimal.Decimal
	LevelNotional      decimal.Decimal
	SideBudget         decimal.Decimal
	InventoryTarget    decimal.Decimal
	InventorySoftLimit decimal.Decimal
	InventoryHardLimit decimal.Decimal
	RepriceBPS         decimal.Decimal
	MinRefreshInterval time.Duration
	MaxQuoteAge        time.Duration
	MaxMarketAge       time.Duration
	HedgeEnabled       bool
	HedgeThreshold     decimal.Decimal
	HedgeMaxWait       time.Duration
	MaxHedgeBacklog    decimal.Decimal
}

// GridParameters uses quote/base prices, quote budgets and base inventory caps.
type GridParameters struct {
	LowerPrice       decimal.Decimal
	UpperPrice       decimal.Decimal
	GridCount        int32
	TotalBudget      decimal.Decimal
	PerOrderNotional decimal.Decimal
	InventoryLimit   decimal.Decimal
	MaxMarketAge     time.Duration
}

// StrategyParameters is a validated, concrete template union for use cases.
type StrategyParameters struct {
	Template StrategyTemplate
	Maker    *MakerParameters
	Grid     *GridParameters
}

// Validate checks maker ranges, total side commitment, inventory and freshness.
// Enablement of market inputs, account funding and minimum order sizes remain
// execution/activation checks against current instrument and risk snapshots.
func (p MakerParameters) Validate() error {
	if p.QuoteLevels < 1 || p.QuoteLevels > MaxQuoteLevels {
		return &ParameterError{Field: "quote_levels"}
	}
	if !p.BaseSpreadBPS.IsPositive() || p.BaseSpreadBPS.GreaterThanOrEqual(decimal.NewFromInt(10000)) {
		return &ParameterError{Field: "base_spread_bps"}
	}
	if p.LevelSpacingBPS.IsNegative() || p.LevelSpacingBPS.GreaterThanOrEqual(decimal.NewFromInt(10000)) || p.QuoteLevels > 1 && !p.LevelSpacingBPS.IsPositive() {
		return &ParameterError{Field: "level_spacing_bps"}
	}
	span := p.BaseSpreadBPS.Add(p.LevelSpacingBPS.Mul(decimal.NewFromInt(2 * int64(p.QuoteLevels-1))))
	if span.GreaterThanOrEqual(decimal.NewFromInt(20000)) {
		return &ParameterError{Field: "level_spacing_bps"}
	}
	if !p.LevelNotional.IsPositive() || p.SideBudget.LessThan(p.LevelNotional.Mul(decimal.NewFromInt(int64(p.QuoteLevels)))) {
		return &ParameterError{Field: "side_budget"}
	}
	if !p.InventorySoftLimit.IsPositive() || p.InventoryHardLimit.LessThan(p.InventorySoftLimit) || p.InventoryTarget.IsNegative() || p.InventoryTarget.GreaterThan(p.InventoryHardLimit) {
		return &ParameterError{Field: "inventory_hard_limit"}
	}
	if !p.RepriceBPS.IsPositive() || p.RepriceBPS.GreaterThanOrEqual(decimal.NewFromInt(10000)) {
		return &ParameterError{Field: "reprice_bps"}
	}
	if !validInterval(p.MinRefreshInterval) || !validInterval(p.MaxQuoteAge) || p.MinRefreshInterval > p.MaxQuoteAge || !validInterval(p.MaxMarketAge) {
		return &ParameterError{Field: "refresh_and_market_age"}
	}
	return validateHedgeParameters(p)
}

func validateHedgeParameters(p MakerParameters) error {
	if p.HedgeEnabled {
		if !p.HedgeThreshold.IsPositive() || p.HedgeThreshold.GreaterThan(p.InventoryHardLimit) || p.MaxHedgeBacklog.LessThan(p.HedgeThreshold) || !validInterval(p.HedgeMaxWait) {
			return &ParameterError{Field: "hedge_limits"}
		}
	} else if !p.HedgeThreshold.IsZero() || !p.MaxHedgeBacklog.IsZero() || p.HedgeMaxWait != 0 {
		return &ParameterError{Field: "disabled_hedge_limits"}
	}
	return nil
}

// Validate checks grid ranges and conservatively funds every configured level.
func (p GridParameters) Validate() error {
	if !p.LowerPrice.IsPositive() || !p.UpperPrice.GreaterThan(p.LowerPrice) {
		return &ParameterError{Field: "grid_price_range"}
	}
	if p.GridCount < 2 || p.GridCount > MaxGridCount {
		return &ParameterError{Field: "grid_count"}
	}
	if !p.PerOrderNotional.IsPositive() || p.TotalBudget.LessThan(p.PerOrderNotional.Mul(decimal.NewFromInt(int64(p.GridCount)))) {
		return &ParameterError{Field: "total_budget"}
	}
	if !p.InventoryLimit.IsPositive() || !validInterval(p.MaxMarketAge) {
		return &ParameterError{Field: "inventory_and_market_age"}
	}
	return nil
}

func validInterval(value time.Duration) bool {
	return value >= time.Millisecond && value <= MaxStrategyInterval && value%time.Millisecond == 0
}
