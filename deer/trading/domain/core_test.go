package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSpotPartialFillsConserveFrozenFundsAndFees(t *testing.T) {
	for _, side := range []Side{SideBuy, SideSell} {
		order := CoreOrder{
			Side:         side,
			Price:        decimal.RequireFromString("101.000000000000000001"),
			Quantity:     decimal.RequireFromString("0.3"),
			QuoteFeeRate: decimal.RequireFromString("0.001"),
		}
		reserve, err := SpotReserve(side, order.Price, order.Quantity, order.QuoteFeeRate)
		if err != nil {
			t.Fatal(err)
		}
		order.FrozenAmount = reserve
		spent, released := decimal.Zero, decimal.Zero
		for range 3 {
			settlement, err := SettleSpotFill(order, order.Price, decimal.RequireFromString("0.1"))
			if err != nil {
				t.Fatal(err)
			}
			spent = spent.Add(settlement.Spent)
			released = released.Add(settlement.Released)
			order.FilledQuantity = order.FilledQuantity.Add(decimal.RequireFromString("0.1"))
			order.FrozenAmount = settlement.RemainingFunds
		}
		if !order.FrozenAmount.IsZero() || !spent.Add(released).Equal(reserve) {
			t.Fatalf("partial fills lost funds: %s + %s != %s", spent, released, reserve)
		}
	}
}

func TestSpotPriceImprovementAndExtremeValues(t *testing.T) {
	order := CoreOrder{
		Side:         SideBuy,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(2),
		QuoteFeeRate: decimal.RequireFromString("0.001"),
		FrozenAmount: decimal.RequireFromString("200.2"),
	}
	settlement, err := SettleSpotFill(order, decimal.NewFromInt(90), decimal.NewFromInt(1))
	if err != nil || !settlement.Released.Equal(decimal.RequireFromString("10.01")) || !settlement.Fee.Equal(decimal.RequireFromString("0.09")) {
		t.Fatalf("price improvement = %+v, %v", settlement, err)
	}
	if _, err := SettleSpotFill(order, decimal.NewFromInt(101), decimal.NewFromInt(1)); err == nil {
		t.Fatal("fill above buy limit was accepted")
	}
	if FixedDecimal(decimal.New(1, 1_000_000_000), 20, 18, false) || FixedDecimal(decimal.New(1, -1_000_000_000), 20, 18, false) {
		t.Fatal("unbounded internal exponent was accepted")
	}
}
