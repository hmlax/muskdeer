package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestPortfolioRebuildOrderFeesAndFullLiquidation(t *testing.T) {
	opening := PortfolioOpening{
		AccountID:    "portfolio-account",
		InstrumentID: 1,
		CashQuote:    decimal.NewFromInt(1000),
	}
	buy := portfolioFactFixture(SideBuy, 1, "100", "2", "0.2")
	sellProfit := portfolioFactFixture(SideSell, 2, "110", "1", "0.11")
	sellLoss := portfolioFactFixture(SideSell, 3, "90", "1", "0.09")
	facts := []FillFact{sellLoss, buy, sellProfit}
	values, err := RebuildPortfolio(opening, facts)
	if err != nil || !values.Quantity.IsZero() || !values.CostQuote.IsZero() || !values.CashQuote.Equal(decimal.RequireFromString("999.6")) ||
		!values.RealizedPnlQuote.Equal(decimal.RequireFromString("-0.4")) || !values.FeesQuote.Equal(decimal.RequireFromString("0.4")) {
		t.Fatalf("rebuilt round trip = %+v, %v", values, err)
	}
	if facts[0].SourceSequence != 3 {
		t.Fatal("rebuild mutated its caller's event order")
	}
	partial, err := RebuildPortfolio(opening, []FillFact{sellProfit, buy})
	if err != nil || !partial.Quantity.Equal(decimal.NewFromInt(1)) || !partial.CostQuote.Equal(decimal.RequireFromString("100.1")) || !partial.RealizedPnlQuote.Equal(decimal.RequireFromString("9.79")) {
		t.Fatalf("average cost and realized PnL = %+v, %v", partial, err)
	}
	if _, err := RebuildPortfolio(opening, []FillFact{sellProfit}); !errors.Is(err, ErrProjectionIncomplete) {
		t.Fatalf("missing inventory was manufactured: %v", err)
	}
}

func TestPortfolioFinalSaleAbsorbsAverageCostRounding(t *testing.T) {
	opening := PortfolioOpening{
		AccountID:    "portfolio-account",
		InstrumentID: 1,
		Quantity:     decimal.NewFromInt(3),
		CostQuote:    decimal.RequireFromString("200.2"),
	}
	values, err := RebuildPortfolio(opening, []FillFact{
		portfolioFactFixture(SideSell, 1, "100", "1", "0"),
		portfolioFactFixture(SideSell, 2, "100", "2", "0"),
	})
	if err != nil || !values.CostQuote.IsZero() || !values.RealizedPnlQuote.Equal(decimal.RequireFromString("99.8")) {
		t.Fatalf("average-cost rounding left unexplained funds: %+v, %v", values, err)
	}
}

func portfolioFactFixture(side Side, sequence int64, price, quantity, fee string) FillFact {
	p, q := decimal.RequireFromString(price), decimal.RequireFromString(quantity)
	return FillFact{
		ExecutionVenue: ExecutionVenueSimulator,
		TradeID:        "test-trade",
		SourceSequence: sequence,
		IntentID:       1,
		CoreOrderID:    1,
		AccountID:      "portfolio-account",
		StrategyID:     1,
		InstrumentID:   1,
		Side:           side,
		Price:          p,
		Quantity:       q,
		QuoteNotional:  p.Mul(q),
		FeeAmount:      decimal.RequireFromString(fee),
		FeeAsset:       "USDT",
	}
}
