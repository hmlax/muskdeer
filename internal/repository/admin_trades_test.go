package repository

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminTradeAmountsRejectInvalidSettlementBeforeDecimalArithmetic(t *testing.T) {
	fill := domain.CoreFill{
		Price:         decimal.RequireFromString("99.99"),
		Quantity:      decimal.NewFromInt(1),
		QuoteNotional: decimal.RequireFromString("99.99"),
		FeeAmount:     decimal.RequireFromString("0.12344444333344436544"),
		FeeAsset:      "USDT",
	}
	order := domain.CoreOrder{
		Side:           domain.SideBuy,
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		FilledQuantity: decimal.NewFromInt(1),
		QuoteFeeRate:   decimal.RequireFromString("0.001234567890123456"),
	}
	if !validAdminTradeAmounts(fill, order, "USDT") {
		t.Fatal("exact price improvement and high precision fee rejected")
	}
	for _, change := range []func(*domain.CoreFill, *domain.CoreOrder){
		func(f *domain.CoreFill, _ *domain.CoreOrder) { f.Price = decimal.New(1, 2147483647) },
		func(f *domain.CoreFill, _ *domain.CoreOrder) { f.Quantity = decimal.New(1, -2147483647) },
		func(f *domain.CoreFill, _ *domain.CoreOrder) { f.FeeAmount = decimal.Zero },
		func(f *domain.CoreFill, _ *domain.CoreOrder) { f.QuoteNotional = decimal.NewFromInt(100) },
		func(f *domain.CoreFill, _ *domain.CoreOrder) { f.FeeAsset = "BTC" },
		func(_ *domain.CoreFill, o *domain.CoreOrder) { o.FilledQuantity = decimal.Zero },
		func(_ *domain.CoreFill, o *domain.CoreOrder) { o.Side = domain.SideSell },
		func(_ *domain.CoreFill, o *domain.CoreOrder) { o.QuoteFeeRate = decimal.NewFromInt(1) },
	} {
		f, o := fill, order
		change(&f, &o)
		if validAdminTradeAmounts(f, o, "USDT") {
			t.Fatal("corrupt financial fact passed authoritative read validation")
		}
	}
	if _, err := NewAdminTrades(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil storage accepted", err)
	}
}
