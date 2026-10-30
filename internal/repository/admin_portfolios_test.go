package repository

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminPortfolioValuesKeepSignedPnLAndRejectHostileOrNegativeBalances(t *testing.T) {
	value := domain.PortfolioSnapshot{
		AccountID:    "portfolio-exact",
		InstrumentID: 1,
		BaseAsset:    "BTC",
		QuoteAsset:   "USDT",
		Values: domain.PortfolioValues{
			RealizedPnlQuote: decimal.RequireFromString("-0.123456789012345678901234567890123456"),
		},
		Revision:  1,
		ErrorCode: domain.PortfolioErrorIncomplete,
	}
	if !validAdminPortfolioSnapshot(value) {
		t.Fatal("signed exact PnL was converted to a negative balance error")
	}
	for _, change := range []func(*domain.PortfolioSnapshot){
		func(v *domain.PortfolioSnapshot) { v.Values.RealizedPnlQuote = decimal.New(1, 2147483647) },
		func(v *domain.PortfolioSnapshot) { v.Values.CashQuote = decimal.NewFromInt(-1) },
		func(v *domain.PortfolioSnapshot) { v.Values.Quantity = decimal.New(1, -2147483647) },
		func(v *domain.PortfolioSnapshot) { v.Opening.CostQuote = decimal.NewFromInt(1) },
		func(v *domain.PortfolioSnapshot) { v.ErrorCode = "other" },
	} {
		candidate := value
		change(&candidate)
		if validAdminPortfolioSnapshot(candidate) {
			t.Fatal("invalid projection values escaped typed read validation")
		}
	}
	if balance, err := adminPortfolioBalance(nil); err != nil || balance != nil {
		t.Fatal("missing core funds became a synthetic balance", err)
	}
	if _, err := NewAdminPortfolios(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil portfolio storage accepted", err)
	}
}
