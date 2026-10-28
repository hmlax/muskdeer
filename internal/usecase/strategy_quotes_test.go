package usecase

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/quoting"
)

func TestFundedTargetsShareOneTotalAndPerOrderCap(t *testing.T) {
	input := domain.StrategyEnvironmentView{
		Instrument: domain.StrategyInstrument{
			QuantityStep: decimal.RequireFromString("0.01"),
			MinNotional:  decimal.NewFromInt(1),
		},
		TotalNotional:        decimal.NewFromInt(15),
		MaximumOrderNotional: decimal.NewFromInt(10),
		MaximumBuyQuantity:   decimal.NewFromInt(10),
	}
	targets := []quoting.Target{
		{
			Side:     domain.SideBuy,
			Price:    decimal.NewFromInt(10),
			Quantity: decimal.NewFromInt(2),
		},
		{
			Side:     domain.SideSell,
			Price:    decimal.NewFromInt(11),
			Quantity: decimal.NewFromInt(2),
		},
	}
	got := fundedTargets(targets, input)
	if len(got) != 2 || got[0].Quantity.String() != "1" || got[1].Quantity.String() != "0.45" || targets[0].Quantity.String() != "2" {
		t.Fatalf("targets did not retain exact lot/cap allocation: %+v", got)
	}
	input.TotalNotional = decimal.RequireFromString("0.99")
	if got := fundedTargets(targets, input); len(got) != 0 {
		t.Fatal("sub-minimum notional target escaped clipping")
	}
	input.TotalNotional = decimal.NewFromInt(100)
	input.MaximumBuyQuantity = decimal.RequireFromString("0.505")
	got = fundedTargets(targets, input)
	if len(got) != 2 || got[0].Quantity.String() != "0.5" || got[1].Quantity.String() != "0.9" {
		t.Fatalf("shared worst-inventory headroom did not clip only buys to exact lots: %+v", got)
	}
}
