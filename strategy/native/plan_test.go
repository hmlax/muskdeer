package native

import (
	"testing"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
)

func TestTargetPlanRoundsAwayFromCrossingAndMergesSameNativePrice(t *testing.T) {
	profile := &commonv1.HimalayaProfilePolicy{
		UserId:               1,
		FundId:               1,
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "1000",
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
		Leverage:             10,
	}
	book := &commonv1.HimalayaBook{
		Symbol:  "BTCUSDT",
		Source:  "binance",
		Segment: "usdm",
		Bids: []*commonv1.HimalayaLevel{
			{Price: "100.19", Quantity: "0.01"},
			{Price: "100.11", Quantity: "0.01"},
		},
		Asks: []*commonv1.HimalayaLevel{
			{Price: "100.21", Quantity: "0.11"},
			{Price: "100.22", Quantity: "0.01"},
		},
	}
	snapshot := &commonv1.HimalayaSnapshot{
		Scope: &commonv1.HimalayaScope{
			UserId: 1,
			FundId: 1,
			Symbol: "BTCUSDT",
		},
		Market: &commonv1.HimalayaMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MinimumQuantity: "0.001",
			MaximumQuantity: "0",
			MinimumNotional: "0.1",
			MaximumLeverage: 10,
		},
	}
	plan, err := BuildTargets(&commonv1.HimalayaPolicy{
		Depth:            20,
		QuantityFraction: "0.1",
	}, profile, book, snapshot)
	if err != nil || len(plan) != 2 || plan[0].Price != "100.1" || plan[0].Quantity != "0.002" || plan[1].Price != "100.3" || plan[1].Quantity != "0.012" {
		t.Fatalf("exact native target conversion failed: %v %v", plan, err)
	}
	snapshot.Market.QuantityStep = "0.01"
	if _, err := BuildTargets(&commonv1.HimalayaPolicy{
		Depth:            20,
		QuantityFraction: "0.1",
	}, profile, book, snapshot); err == nil {
		t.Fatal("native grid changed without a configuration restart")
	}
}
