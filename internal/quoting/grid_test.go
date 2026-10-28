package quoting

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestGridKeepsFixedLevelsWithinActualFundsAndTotalBudget(t *testing.T) {
	input := gridFixture()
	targets, err := Grid(input)
	if err != nil || len(targets) != 4 {
		t.Fatalf("grid levels: %+v, %v", targets, err)
	}
	total, cash, base, buys := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
	prices := []string{"98", "99", "101", "102"}
	for i, target := range targets {
		if target.Price.String() != prices[i] || !target.Quantity.Mod(input.Instrument.QuantityStep).IsZero() || target.TimeInForce != domain.TimeInForcePostOnly {
			t.Fatalf("grid changed a fixed level or precision: %+v", target)
		}
		notional := target.Price.Mul(target.Quantity)
		total = total.Add(notional)
		if target.Side == domain.SideBuy {
			cash = cash.Add(notional.Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)))
			buys = buys.Add(target.Quantity)
		} else {
			base = base.Add(target.Quantity)
		}
	}
	if total.GreaterThan(input.Parameters.TotalBudget) || cash.GreaterThan(input.AvailableQuote) || base.GreaterThan(input.AvailableBase) || input.Inventory.Add(buys).GreaterThan(input.Parameters.InventoryLimit) {
		t.Fatal("grid exceeded funds, fees, total budget or worst-case inventory")
	}
}

func TestGridProtectsOutsideRangeAndNeverRepricesCrossingLevels(t *testing.T) {
	input := gridFixture()
	input.Parameters.LowerPrice = decimal.NewFromInt(101)
	input.Parameters.UpperPrice = decimal.NewFromInt(105)
	if targets, err := Grid(input); !errors.Is(err, ErrGridOutsideRange) || len(targets) != 0 {
		t.Fatal("outside-range grid did not require protection")
	}
	input = gridFixture()
	input.LocalAsk = decimal.RequireFromString("98.5")
	input.LocalBid = decimal.RequireFromString("101.5")
	if targets, err := Grid(input); err == nil || len(targets) != 0 {
		t.Fatal("crossed local book accepted")
	}
	input.LocalBid = decimal.NewFromInt(98)
	targets, err := Grid(input)
	if err != nil || len(targets) != 3 || targets[0].Price.String() != "98" {
		t.Fatalf("grid moved an unsafe fixed price rather than skipping it: %+v, %v", targets, err)
	}
}

func TestGridRationalLevelsAndUnfundedSides(t *testing.T) {
	input := gridFixture()
	input.Instrument.PriceTick = decimal.RequireFromString("0.000000000000000001")
	input.Parameters.LowerPrice = decimal.RequireFromString("99.000000000000000001")
	input.Parameters.UpperPrice = decimal.RequireFromString("100.000000000000000001")
	input.Parameters.GridCount = 4
	targets, err := Grid(input)
	if err != nil || len(targets) != 4 || targets[1].Price.String() != "99.333333333333333334" {
		t.Fatalf("rational grid spacing rounded early: %+v, %v", targets, err)
	}
	input = gridFixture()
	input.AvailableBase, input.Inventory = decimal.Zero, decimal.Zero
	targets, err = Grid(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Side == domain.SideSell {
			t.Fatal("grid invented short inventory")
		}
	}
	input = gridFixture()
	input.Inventory = input.Parameters.InventoryLimit
	targets, err = Grid(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Side == domain.SideBuy {
			t.Fatal("grid netted sells against hard inventory")
		}
	}
}

func TestGridCapsCombinedBuysBeforeAnySellCanReleaseInventory(t *testing.T) {
	input := gridFixture()
	input.AvailableQuote = decimal.NewFromInt(5)
	input.AvailableBase = decimal.RequireFromString("0.03")
	input.Instrument.MinNotional = decimal.RequireFromString("0.1")
	input.Parameters.InventoryLimit = input.Inventory.Add(decimal.RequireFromString("0.01"))
	targets, err := Grid(input)
	if err != nil {
		t.Fatal(err)
	}
	buys, sells, cash := decimal.Zero, decimal.Zero, decimal.Zero
	for _, target := range targets {
		if target.Side == domain.SideBuy {
			buys = buys.Add(target.Quantity)
			cash = cash.Add(target.Price.Mul(target.Quantity).Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)))
		} else {
			sells = sells.Add(target.Quantity)
		}
	}
	if buys.IsZero() || sells.IsZero() || buys.GreaterThan(decimal.RequireFromString("0.01")) || sells.GreaterThan(input.AvailableBase) || cash.GreaterThan(input.AvailableQuote) {
		t.Fatal("grid borrowed unconfirmed sells, frozen base or fee cash")
	}
}

func TestGridSupportsTwoHundredDistinctLevelsAndRejectsBadInputs(t *testing.T) {
	input := gridFixture()
	input.Parameters.GridCount = domain.MaxGridCount
	input.Parameters.TotalBudget = decimal.NewFromInt(2000)
	input.AvailableBase, input.AvailableQuote = decimal.NewFromInt(100), decimal.NewFromInt(10000)
	input.Parameters.InventoryLimit = decimal.NewFromInt(1000)
	targets, err := Grid(input)
	if err != nil || len(targets) != domain.MaxGridCount {
		t.Fatalf("bounded grid: %d, %v", len(targets), err)
	}
	if _, err := Diff(DiffInput{
		StrategyID:         1,
		AccountID:          "grid-test",
		InstrumentID:       input.Instrument.ID,
		Targets:            targets,
		Now:                input.Now,
		RepriceBPS:         decimal.Zero,
		MinRefreshInterval: input.Parameters.MaxMarketAge,
		MaxQuoteAge:        input.Parameters.MaxMarketAge,
	}); err != nil {
		t.Fatalf("diff rejected valid bounded grid levels: %v", err)
	}
	for _, mutate := range []func(*GridInput){
		func(v *GridInput) { v.Market.Reading.Source = domain.MarketSourceOKX },
		func(v *GridInput) { v.Market.Reading.ValidUntil = v.Now },
		func(v *GridInput) { v.Parameters.GridCount = 201 },
		func(v *GridInput) { v.Parameters.UpperPrice = decimal.NewFromInt(98) },
		func(v *GridInput) { v.Parameters.TotalBudget = decimal.NewFromInt(1) },
		func(v *GridInput) { v.Parameters.PerOrderNotional = decimal.New(1, 2000000000) },
		func(v *GridInput) { v.Instrument.PriceTick = decimal.NewFromInt(10) },
	} {
		input := gridFixture()
		mutate(&input)
		if targets, err := Grid(input); err == nil || len(targets) != 0 {
			t.Fatal("invalid grid produced targets")
		}
	}
}

func gridFixture() GridInput {
	maker := makerFixture()
	return GridInput{
		Instrument:     maker.Instrument,
		Source:         maker.Source,
		Symbol:         maker.Symbol,
		Mode:           maker.Mode,
		Market:         maker.Market,
		Inventory:      maker.Inventory,
		AvailableBase:  maker.AvailableBase,
		AvailableQuote: maker.AvailableQuote,
		QuoteFeeRate:   maker.QuoteFeeRate,
		Now:            maker.Now,
		Parameters: domain.GridParameters{
			LowerPrice:       decimal.NewFromInt(98),
			UpperPrice:       decimal.NewFromInt(102),
			GridCount:        5,
			TotalBudget:      decimal.NewFromInt(50),
			PerOrderNotional: decimal.NewFromInt(10),
			InventoryLimit:   decimal.NewFromInt(20),
			MaxMarketAge:     maker.Parameters.MaxMarketAge,
		},
	}
}
