package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// StrategyInstrument contains current execution precision, independent of the
// selected external market symbol in StrategyEnvironmentView.
type StrategyInstrument struct {
	ID              int64
	MetadataVersion int64
	Status          string
	PriceTick       decimal.Decimal
	QuantityStep    decimal.Decimal
	MinNotional     decimal.Decimal
}

// StrategyEnvironmentView is a read-only planning observation, not a reserve
// or a grant. TotalNotional includes this strategy's existing reservations but
// subtracts other strategies/accounts in every shared scope.
type StrategyEnvironmentView struct {
	Instrument           StrategyInstrument
	Parameters           StrategyParameters
	Symbol               string
	BaseAsset            string
	QuoteAsset           string
	QuoteFeeRate         decimal.Decimal
	TotalNotional        decimal.Decimal
	MaximumOrderNotional decimal.Decimal
	LocalBid             decimal.Decimal
	LocalAsk             decimal.Decimal
	RiskAllowed          bool
	RiskReason           RiskReason
	ObservedAt           time.Time
	LastRefresh          time.Time
}

// StrategyEnvironment checks the exact supplied active spec against a coherent
// database observation. It never activates configuration or seeds resources.
type StrategyEnvironment interface {
	Current(ctx context.Context, spec StrategySpec) (StrategyEnvironmentView, error)
}
