package quoting

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func FuzzMakerAllocationsNeverExceedCashBaseInventoryOrSideLimits(f *testing.F) {
	f.Add(uint64(10), uint64(10), uint64(100), uint64(100), uint64(1))
	f.Add(uint64(0), uint64(0), uint64(0), uint64(1), uint64(20))
	f.Add(uint64(30), uint64(10), uint64(1), uint64(10000), uint64(7))
	f.Fuzz(func(t *testing.T, inventory, base, cash, price, levels uint64) {
		input := makerFixture()
		input.Inventory = decimal.NewFromInt(int64(inventory % 31))
		input.AvailableBase = decimal.NewFromInt(int64(base % 100))
		input.AvailableQuote = decimal.NewFromInt(int64(cash % 10000))
		input.Market.Reading.Mid = decimal.NewFromInt(1 + int64(price%10000))
		input.Market.Reading.Bid = input.Market.Reading.Mid.Sub(input.Instrument.PriceTick)
		input.Market.Reading.Ask = input.Market.Reading.Mid.Add(input.Instrument.PriceTick)
		input.Parameters.QuoteLevels = 1 + int32(levels%20)
		input.Parameters.SideBudget = input.Parameters.LevelNotional.Mul(decimal.NewFromInt(int64(input.Parameters.QuoteLevels)))
		targets, err := Maker(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) > 2*int(input.Parameters.QuoteLevels) {
			t.Fatal("unbounded maker target count")
		}
		buy, sell, cost, bought, sold := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
		for _, target := range targets {
			if !domain.FixedDecimal(target.Price, 20, 18, false) || !domain.FixedDecimal(target.Quantity, 20, 18, false) ||
				!target.Price.Mod(input.Instrument.PriceTick).IsZero() || !target.Quantity.Mod(input.Instrument.QuantityStep).IsZero() ||
				target.TimeInForce != domain.TimeInForcePostOnly || target.Price.Mul(target.Quantity).LessThan(input.Instrument.MinNotional) {
				t.Fatal("invalid precision, minimum or passive maker target")
			}
			notional := target.Price.Mul(target.Quantity)
			switch target.Side {
			case domain.SideBuy:
				buy, bought = buy.Add(notional), bought.Add(target.Quantity)
				cost = cost.Add(notional.Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)))
			case domain.SideSell:
				sell, sold = sell.Add(notional), sold.Add(target.Quantity)
			default:
				t.Fatal("unknown target side")
			}
		}
		if cost.GreaterThan(input.AvailableQuote) || sold.GreaterThan(input.AvailableBase) || sold.GreaterThan(input.Inventory) ||
			buy.GreaterThan(input.Parameters.SideBudget) || sell.GreaterThan(input.Parameters.SideBudget) ||
			bought.IsPositive() && input.Inventory.Add(bought).GreaterThan(input.Parameters.InventoryHardLimit) {
			t.Fatal("maker allocation exceeded exact cash/base/inventory/budget limits")
		}
	})
}
