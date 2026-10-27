package domain

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/shopspring/decimal"
)

// Projection bounds and stable status identifiers.
const (
	PortfolioConsumer             = "portfolio.v1"
	PortfolioErrorIncomplete      = "SOURCE_INCOMPLETE"
	PortfolioErrorMismatch        = "RECONCILIATION_MISMATCH"
	MaxPortfolioFacts             = 10_000
	MessageProjectionIncomplete   = "portfolio source facts are incomplete"
	MessageReconciliationMismatch = "portfolio differs from authoritative core balances"
)

// Projection errors never authorize manufactured balancing entries.
var (
	ErrProjectionIncomplete   = errors.New(MessageProjectionIncomplete)
	ErrReconciliationMismatch = errors.New(MessageReconciliationMismatch)
)

// PortfolioOpening is explicitly supplied before trading; cost is not inferred
// from a later market price. One instrument is bound to each MVP account.
type PortfolioOpening struct {
	AccountID    string
	InstrumentID int64
	Quantity     decimal.Decimal
	CashQuote    decimal.Decimal
	CostQuote    decimal.Decimal
}

// FillFact keeps natural trade identity independently of delivery identity.
type FillFact struct {
	ExecutionVenue string
	TradeID        string
	SourceSequence int64
	IntentID       int64
	CoreOrderID    int64
	AccountID      string
	StrategyID     int64
	InstrumentID   int64
	Side           Side
	Price          decimal.Decimal
	Quantity       decimal.Decimal
	QuoteNotional  decimal.Decimal
	FeeAmount      decimal.Decimal
	FeeAsset       string
}

// FillNotice binds a validated wire event to its durable inbox fingerprint.
type FillNotice struct {
	EventID     string
	PayloadHash string
	Fact        FillFact
}

// PortfolioValues use quote fees and average cost with explicit 36-place
// truncation; a final sale removes the full residual cost without dust.
type PortfolioValues struct {
	Quantity         decimal.Decimal
	CashQuote        decimal.Decimal
	CostQuote        decimal.Decimal
	RealizedPnlQuote decimal.Decimal
	FeesQuote        decimal.Decimal
}

// PortfolioSnapshot is a rebuildable projection and its reconciliation state.
type PortfolioSnapshot struct {
	AccountID          string
	InstrumentID       int64
	BaseAsset          string
	QuoteAsset         string
	Opening            PortfolioOpening
	Values             PortfolioValues
	LastSourceSequence int64
	Reconciled         bool
	Revision           int64
	ErrorCode          string
}

// PortfolioRepository commits inbox, natural trade facts and projection before
// acknowledgement. Rebuild never changes authoritative core assets.
type PortfolioRepository interface {
	Initialize(ctx context.Context, opening PortfolioOpening) (PortfolioSnapshot, error)
	Apply(ctx context.Context, notice FillNotice) (PortfolioSnapshot, error)
	Get(ctx context.Context, accountID string) (PortfolioSnapshot, error)
	Rebuild(ctx context.Context, accountID string) (PortfolioSnapshot, error)
}

// ValidateFillFact bounds all decimal operands before any arithmetic.
func ValidateFillFact(fact FillFact) error {
	if fact.ExecutionVenue != ExecutionVenueSimulator || fact.SourceSequence <= 0 || fact.IntentID <= 0 || fact.CoreOrderID <= 0 || fact.InstrumentID <= 0 || fact.StrategyID <= 0 ||
		!FixedDecimal(fact.Price, 20, 18, false) || !FixedDecimal(fact.Quantity, 20, 18, false) ||
		!FixedDecimal(fact.QuoteNotional, 40, 36, false) || !FixedDecimal(fact.FeeAmount, 40, 36, true) ||
		fact.FeeAmount.GreaterThanOrEqual(fact.QuoteNotional) || !fact.QuoteNotional.Equal(fact.Price.Mul(fact.Quantity)) ||
		(fact.Side != SideBuy && fact.Side != SideSell) {
		return ErrInvalidOrder
	}
	return nil
}

// RebuildPortfolio sorts a private copy, so delivery order cannot change cost
// basis. Incomplete inventory/cash is retained for reconciliation, never filled
// with a synthetic zero balance or silently skipped execution.
func RebuildPortfolio(opening PortfolioOpening, facts []FillFact) (PortfolioValues, error) {
	if !FixedDecimal(opening.Quantity, 40, 36, true) || !FixedDecimal(opening.CashQuote, 40, 36, true) || !FixedDecimal(opening.CostQuote, 40, 36, true) ||
		opening.Quantity.IsZero() && !opening.CostQuote.IsZero() || len(facts) > MaxPortfolioFacts {
		return PortfolioValues{}, ErrInvalidOrder
	}
	ordered := append([]FillFact(nil), facts...)
	slices.SortFunc(ordered, func(a, b FillFact) int { return cmp.Compare(a.SourceSequence, b.SourceSequence) })
	values := PortfolioValues{
		Quantity:  opening.Quantity,
		CashQuote: opening.CashQuote,
		CostQuote: opening.CostQuote,
	}
	var previous int64
	for _, fact := range ordered {
		if err := ValidateFillFact(fact); err != nil {
			return PortfolioValues{}, err
		}
		if fact.SourceSequence <= previous || fact.AccountID != opening.AccountID || fact.InstrumentID != opening.InstrumentID {
			return PortfolioValues{}, ErrInvalidOrder
		}
		previous = fact.SourceSequence
		if err := applyPortfolioFill(&values, fact); err != nil {
			return PortfolioValues{}, err
		}
	}
	return values, nil
}

func applyPortfolioFill(values *PortfolioValues, fact FillFact) error {
	if fact.Side == SideBuy {
		spent := fact.QuoteNotional.Add(fact.FeeAmount)
		if spent.GreaterThan(values.CashQuote) {
			return ErrProjectionIncomplete
		}
		values.Quantity = values.Quantity.Add(fact.Quantity)
		values.CashQuote = values.CashQuote.Sub(spent)
		values.CostQuote = values.CostQuote.Add(spent)
	} else {
		if fact.Quantity.GreaterThan(values.Quantity) {
			return ErrProjectionIncomplete
		}
		removed := values.CostQuote
		if !fact.Quantity.Equal(values.Quantity) {
			removed, _ = values.CostQuote.Mul(fact.Quantity).QuoRem(values.Quantity, 36)
		}
		received := fact.QuoteNotional.Sub(fact.FeeAmount)
		values.Quantity = values.Quantity.Sub(fact.Quantity)
		values.CashQuote = values.CashQuote.Add(received)
		values.CostQuote = values.CostQuote.Sub(removed)
		values.RealizedPnlQuote = values.RealizedPnlQuote.Add(received.Sub(removed))
	}
	values.FeesQuote = values.FeesQuote.Add(fact.FeeAmount)
	if !FixedDecimal(values.Quantity, 40, 36, true) || !FixedDecimal(values.CashQuote, 40, 36, true) || !FixedDecimal(values.CostQuote, 40, 36, true) ||
		!FixedDecimal(values.RealizedPnlQuote.Abs(), 40, 36, true) || !FixedDecimal(values.FeesQuote, 40, 36, true) {
		return ErrInvalidOrder
	}
	return nil
}
