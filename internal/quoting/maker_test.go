package quoting

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestMakerQuantizesExactTargetsWithoutExceedingSideOrCashBudgets(t *testing.T) {
	input := makerFixture()
	input.Parameters.QuoteLevels = 2
	input.Parameters.SideBudget = decimal.NewFromInt(20)
	targets, err := Maker(input)
	if err != nil || len(targets) != 4 {
		t.Fatalf("maker targets: %+v, %v", targets, err)
	}
	buy, sell := decimal.Zero, decimal.Zero
	for _, target := range targets {
		if !target.Price.Mod(input.Instrument.PriceTick).IsZero() || !target.Quantity.Mod(input.Instrument.QuantityStep).IsZero() || target.TimeInForce != domain.TimeInForcePostOnly || target.Price.Mul(target.Quantity).LessThan(input.Instrument.MinNotional) {
			t.Fatalf("target violated precision/minimum/passive policy: %+v", target)
		}
		if target.Side == domain.SideBuy {
			buy = buy.Add(target.Price.Mul(target.Quantity))
		} else {
			sell = sell.Add(target.Price.Mul(target.Quantity))
		}
	}
	if buy.GreaterThan(input.Parameters.SideBudget) || sell.GreaterThan(input.Parameters.SideBudget) || buy.Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)).GreaterThan(input.AvailableQuote) {
		t.Fatal("target side or quote fee budget exceeded")
	}
	if targets[0].Price.String() != "99.9" || targets[1].Price.String() != "100.1" || targets[2].Price.String() != "99.8" || targets[3].Price.String() != "100.2" {
		t.Fatalf("spread/level semantics changed: %+v", targets)
	}
}

func TestMakerCashFeeInventoryAndLocalBookAreHardBounds(t *testing.T) {
	input := makerFixture()
	input.AvailableQuote = decimal.NewFromInt(5)
	input.AvailableBase = decimal.RequireFromString("0.03")
	input.LocalAsk = decimal.RequireFromString("99.9")
	input.LocalBid = decimal.RequireFromString("99.8")
	targets, err := Maker(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Side == domain.SideBuy && (!target.Price.LessThan(input.LocalAsk) || target.Price.Mul(target.Quantity).Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)).GreaterThan(input.AvailableQuote)) {
			t.Fatal("buy crossed local book or fee-adjusted available cash")
		}
		if target.Side == domain.SideSell && (!target.Price.GreaterThan(input.LocalBid) || target.Quantity.GreaterThan(input.AvailableBase)) {
			t.Fatal("sell crossed local book or available base")
		}
	}
	input = makerFixture()
	input.Inventory = input.Parameters.InventoryHardLimit
	targets, err = Maker(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Side == domain.SideBuy {
			t.Fatal("hard inventory cap still allowed a buy")
		}
	}
	input.Inventory, input.AvailableBase = decimal.Zero, decimal.Zero
	targets, err = Maker(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Side == domain.SideSell {
			t.Fatal("spot maker invented short inventory")
		}
	}
}

func TestMakerUsesExactRationalInventorySkewBeforeTickRounding(t *testing.T) {
	input := makerFixture()
	input.Instrument.PriceTick = decimal.RequireFromString("0.000000000000000001")
	input.Inventory = decimal.NewFromInt(11)
	input.Parameters.InventorySoftLimit = decimal.NewFromInt(3)
	targets, err := Maker(input)
	if err != nil || len(targets) != 2 {
		t.Fatalf("inventory skew: %+v, %v", targets, err)
	}
	if targets[0].Price.String() != "99.866666666666666666" || targets[1].Price.String() != "100.066666666666666667" {
		t.Fatalf("inventory division rounded before price tick: %+v", targets)
	}
}

func TestUnselectedStaleUnreliableOrUnfundedMarketCannotProduceTargets(t *testing.T) {
	for _, mutate := range []func(*Input){
		func(value *Input) { value.Market.Reading.Source = domain.MarketSourceOKX },
		func(value *Input) { value.Market.Reading.Quality = domain.MarketQualityOutlier },
		func(value *Input) { value.Market.Reading.ValidUntil = value.Now },
		func(value *Input) { value.Market.AuthorityUntil = value.Now },
		func(value *Input) { value.Mode = domain.MarketModeLive },
		func(value *Input) { value.Instrument.QuantityStep = decimal.New(1, 2000000000) },
		func(value *Input) { value.Instrument.Status = domain.InstrumentStatusHalted },
		func(value *Input) { value.Parameters.BaseSpreadBPS = decimal.New(1, 2000000000) },
	} {
		input := makerFixture()
		mutate(&input)
		if targets, err := Maker(input); !errors.Is(err, ErrInvalidInput) || len(targets) != 0 {
			t.Fatal("unsafe input produced maker targets")
		}
	}
	input := makerFixture()
	input.AvailableBase, input.AvailableQuote = decimal.Zero, decimal.Zero
	if targets, err := Maker(input); err != nil || len(targets) != 0 {
		t.Fatalf("unfunded plan produced orders: %+v, %v", targets, err)
	}
	input = makerFixture()
	input.Instrument.MinNotional = decimal.NewFromInt(20)
	if targets, err := Maker(input); err != nil || len(targets) != 0 {
		t.Fatalf("sub-minimum plan produced orders: %+v, %v", targets, err)
	}
}

func makerFixture() Input {
	now := time.Now().UTC()
	return Input{
		Instrument: Instrument{
			ID:              1,
			MetadataVersion: 1,
			Status:          domain.InstrumentStatusTrading,
			PriceTick:       decimal.RequireFromString("0.01"),
			QuantityStep:    decimal.RequireFromString("0.001"),
			MinNotional:     decimal.NewFromInt(1),
		},
		Source: domain.MarketSourceBinance,
		Symbol: "BTCUSDT",
		Mode:   domain.MarketModeReplay,
		Market: domain.MarketReference{
			FeedID:         1,
			OwnerID:        "market.01234567890123456789012345678901",
			OwnerEpoch:     1,
			Revision:       1,
			AuthorityUntil: now.Add(3 * time.Second),
			Reading: domain.MarketReading{
				Source:        domain.MarketSourceBinance,
				InstrumentID:  1,
				Symbol:        "BTCUSDT",
				Mode:          domain.MarketModeReplay,
				SourceVersion: domain.MarketSourceVersion,
				Connection:    1,
				Generation:    1,
				Sequence:      1,
				Quality:       domain.MarketQualityHealthy,
				Reason:        domain.MarketReasonNone,
				Bid:           decimal.RequireFromString("99.99"),
				Ask:           decimal.RequireFromString("100.01"),
				BidQuantity:   decimal.NewFromInt(1),
				AskQuantity:   decimal.NewFromInt(1),
				Mid:           decimal.NewFromInt(100),
				EventTime:     now.Add(-time.Millisecond),
				ReceivedAt:    now,
				EvaluatedAt:   now,
				ValidUntil:    now.Add(time.Second),
			},
		},
		Parameters: domain.MakerParameters{
			QuoteLevels:        1,
			LevelSpacingBPS:    decimal.NewFromInt(10),
			BaseSpreadBPS:      decimal.NewFromInt(20),
			LevelNotional:      decimal.NewFromInt(10),
			SideBudget:         decimal.NewFromInt(10),
			InventoryTarget:    decimal.NewFromInt(10),
			InventorySoftLimit: decimal.NewFromInt(2),
			InventoryHardLimit: decimal.NewFromInt(20),
			RepriceBPS:         decimal.NewFromInt(1),
			MinRefreshInterval: 100 * time.Millisecond,
			MaxQuoteAge:        time.Second,
			MaxMarketAge:       time.Second,
		},
		Inventory:      decimal.NewFromInt(10),
		AvailableBase:  decimal.NewFromInt(10),
		AvailableQuote: decimal.NewFromInt(100),
		QuoteFeeRate:   decimal.RequireFromString("0.001"),
		Now:            now,
	}
}
