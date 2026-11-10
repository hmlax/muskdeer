package longgrid

import (
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/shopspring/decimal"
)

func TestLongGridStartsFor100NotionalUsesOnePercentAndStopsBeforeAdd(t *testing.T) {
	rules := testRules()
	snapshot := testSnapshot("0", "0", "100")
	decision, err := Decide(rules, &common.LongGridLots{}, decimal.Zero, false, snapshot, "99.9", "100", true, false)
	if err != nil || decision.Kind != InitialBuy || decision.Side != domain.SideBuy || !decision.Quantity.Equal(decimal.NewFromInt(1)) || !decision.Price.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("initial: %+v, %v", decision, err)
	}
	lots := &common.LongGridLots{Items: []*common.LongGridLot{{
		Sequence:   1,
		EntryPrice: "100",
		Quantity:   "1",
	}}}
	snapshot = testSnapshot("1", "100", "99.6")
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "99.6", "99.7", true, false)
	if err != nil || decision.Kind != Hold {
		t.Fatalf("above stop: %+v, %v", decision, err)
	}
	snapshot = testSnapshot("1", "100", "99.5")
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "99.5", "99.6", true, false)
	if err != nil || decision.Kind != StopLoss || decision.Side != domain.SideSell || !decision.Quantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("stop at 0.5%%: %+v, %v", decision, err)
	}
	snapshot = testSnapshot("1", "100", "98")
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "98", "98.1", true, false)
	if err != nil || decision.Kind != StopLoss {
		t.Fatalf("stop must precede 1%% add: %+v, %v", decision, err)
	}
	snapshot = testSnapshot("1", "100", "101")
	snapshot.Market.BestBid = "101"
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "101", "101.1", true, false)
	if err != nil || decision.Kind != TakeProfit || decision.LotSequence != 1 || !decision.Quantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("profit: %+v, %v", decision, err)
	}
}

func TestLongGridCannotOpenShortOrSellMoreThanLotOrReplayInitial(t *testing.T) {
	rules := testRules()
	snapshot := testSnapshot("0", "0", "101")
	decision, err := Decide(rules, &common.LongGridLots{}, decimal.NewFromInt(100), true, snapshot, "101", "101.1", true, false)
	if err != nil || decision.Kind != Hold {
		t.Fatalf("flat above buyback: %+v, %v", decision, err)
	}
	snapshot = testSnapshot("0.4", "100", "101")
	snapshot.Market.BestBid = "101"
	lots := &common.LongGridLots{Items: []*common.LongGridLot{{
		Sequence:   1,
		EntryPrice: "100",
		Quantity:   "0.4",
	}}}
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "101", "101.1", true, false)
	if err != nil || decision.Kind != TakeProfit || decision.Quantity.String() != "0.4" {
		t.Fatalf("partial lot exit: %+v, %v", decision, err)
	}
	snapshot.Positions[0].SignedQuantity = "-0.4"
	if _, err := Decide(rules, lots, decimal.Zero, true, snapshot, "101", "101.1", true, false); err == nil {
		t.Fatal("short position accepted")
	}
	snapshot = testSnapshot("0", "0", "100")
	decision, err = Decide(rules, &common.LongGridLots{}, decimal.Zero, false, snapshot, "99.9", "100", false, false)
	if err != nil || decision.Kind != Hold {
		t.Fatalf("disabled opened: %+v, %v", decision, err)
	}
}

func testRules() Rules {
	return Rules{
		Spacing: decimal.RequireFromString("0.01"),
		Stop:    decimal.RequireFromString("0.005"),
		Order:   decimal.NewFromInt(100),
		Maximum: decimal.NewFromInt(1000),
	}
}
func testSnapshot(quantity, entry, mark string) *common.VenueSnapshot {
	result := &common.VenueSnapshot{
		Scope: &common.VenueScope{Symbol: "BTCUSDT"},
		Market: &common.VenueMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MinimumQuantity: "0.001",
			MinimumNotional: "1",
			MaximumQuantity: "100",
			MarkPrice:       mark,
			BestBid:         "99.9",
			BestAsk:         "100",
		},
	}
	if quantity != "0" {
		result.Positions = []*common.VenuePosition{{
			Symbol:         "BTCUSDT",
			PositionId:     "long-position",
			SignedQuantity: quantity,
			EntryPrice:     entry,
			Leverage:       "10",
		}}
	}
	return result
}

func TestLongGridOnePercentBuybackAndFixedNotional(t *testing.T) {
	rules := testRules()
	rules.Stop = decimal.RequireFromString("0.02")
	lots := &common.LongGridLots{Items: []*common.LongGridLot{{
		Sequence:   1,
		EntryPrice: "100",
		Quantity:   "1",
	}}}
	snapshot := testSnapshot("1", "100", "98.9")
	snapshot.Market.BestAsk = "99"
	decision, err := Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "98.9", "99", true, false)
	if err != nil || decision.Kind != GridBuy || decision.Price.String() != "99" || decision.Price.Mul(decision.Quantity).GreaterThan(decimal.NewFromInt(100)) {
		t.Fatalf("one percent grid buy: %+v, %v", decision, err)
	}
	decision, err = Decide(rules, lots, decimal.NewFromInt(99), true, snapshot, "99", "99.1", true, false)
	if err != nil || decision.Kind != Hold {
		t.Fatalf("above grid trigger: %+v, %v", decision, err)
	}
}

func TestFreshSelectedBidStopsBeforeAddEvenIfVenueMarkHasNotMoved(t *testing.T) {
	snapshot := testSnapshot("1", "100", "100")
	lots := &common.LongGridLots{Items: []*common.LongGridLot{{
		Sequence:   1,
		EntryPrice: "100",
		Quantity:   "1",
	}}}
	decision, err := Decide(testRules(), lots, decimal.NewFromInt(99), true, snapshot, "99.4", "99.5", true, false)
	if err != nil || decision.Kind != StopLoss {
		t.Fatalf("fresh selected source was ignored: %+v, %v", decision, err)
	}
}
