package console

import (
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/protobuf/proto"
)

func makerFixture() (*common.VenueProfilePolicy, *common.DepthMakerParameters, *common.VenueBook, *common.VenueSnapshot) {
	now := time.Now().UnixMilli()
	profile := &common.VenueProfilePolicy{
		UserId:               1,
		FundId:               1,
		StrategyInstanceId:   213,
		Symbol:               "LINKUSDT",
		MaximumNotional:      "100",
		MaximumOrderNotional: "100",
		PriceTick:            "0.001",
		QuantityStep:         "0.01",
		Leverage:             10,
		MarketDataSource:     values.Source,
	}
	params := &common.DepthMakerParameters{
		Depth:            20,
		QuantityFraction: "0.1",
	}
	book := &common.VenueBook{
		Symbol:       profile.Symbol,
		Source:       values.Source,
		Segment:      values.Segment,
		ValidUntilMs: now + 5000,
		Bids: []*common.VenueLevel{
			{
				Price:    "12",
				Quantity: "1000",
			},
		},
		Asks: []*common.VenueLevel{
			{
				Price:    "13",
				Quantity: "1000",
			},
		},
	}
	snapshot := &common.VenueSnapshot{
		Scope:        values.Scope(profile),
		Available:    "2000000",
		ObservedAtMs: now,
		ValidUntilMs: now + 5000,
		Market: &common.VenueMarket{
			PriceTick:       profile.PriceTick,
			QuantityStep:    profile.QuantityStep,
			MinimumQuantity: "0.01",
			MinimumNotional: "1",
			MaximumQuantity: "0",
			MaximumLeverage: 10,
		},
		Orders: []*common.VenueOrder{
			{
				ClientId: "deer_LINKUSDT_s213_1",
				Side:     string(domain.SideBuy),
			},
			{
				ClientId: "deer_LINKUSDT_s999_1",
				Side:     string(domain.SideSell),
			},
		},
	}
	return profile, params, book, snapshot
}

func TestMakerCheckUsesActualClippedTargetsAndOwnOrders(t *testing.T) {
	profile, params, book, snapshot := makerFixture()
	result := evaluateMaker(profile, params, book, snapshot, time.Now().UnixMilli())
	if result.CanStart || result.Reason != domain.MakerBudgetReason || result.TargetNotional != "199.93" || result.TargetOrderCount != 2 || result.ActiveBuyOrders != 1 || result.ActiveSellOrders != 0 {
		t.Fatalf("underfunded maker accepted or foreign order counted: %v", result)
	}
	profile.MaximumNotional = "4200"
	if result := evaluateMaker(profile, params, book, snapshot, time.Now().UnixMilli()); !result.CanStart {
		t.Fatalf("sufficient budget rejected: %v", result)
	}
	profile.MaximumNotional = "100"
	book.Bids[0].Quantity, book.Asks[0].Quantity = "1", "1"
	if result := evaluateMaker(profile, params, book, snapshot, time.Now().UnixMilli()); !result.CanStart || result.TargetNotional != "2.5" {
		t.Fatalf("must use actual source sizes instead of depth times cap: %v", result)
	}
}

func TestMakerCheckRejectsStaleForeignAndOneSidedInputs(t *testing.T) {
	profile, params, book, snapshot := makerFixture()
	now := time.Now().UnixMilli()
	for _, change := range []func(*common.VenueBook, *common.VenueSnapshot){
		func(b *common.VenueBook, _ *common.VenueSnapshot) { b.ValidUntilMs = now },
		func(b *common.VenueBook, _ *common.VenueSnapshot) { b.Asks = nil },
		func(b *common.VenueBook, _ *common.VenueSnapshot) { b.Source = "other" },
		func(_ *common.VenueBook, s *common.VenueSnapshot) { s.ValidUntilMs = now },
		func(_ *common.VenueBook, s *common.VenueSnapshot) { s.Scope.FundId++ },
		func(_ *common.VenueBook, s *common.VenueSnapshot) { s.Market.QuantityStep = "1" },
	} {
		b, s := proto.CloneOf(book), proto.CloneOf(snapshot)
		change(b, s)
		if result := evaluateMaker(profile, params, b, s, now); result.CanStart {
			t.Fatalf("invalid source or observation accepted: %v", result)
		}
	}
}
