package himalaya

import (
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
)

func TestLongGridRetainsHedgePositionIDWithoutWeakeningLegacyMode(t *testing.T) {
	scope := Scope{
		UserID: 20000,
		FundID: 20000,
		Symbol: "BTCUSDT",
	}
	input := &SDKSnapshot{
		Account: &pb.GetAccountResponse{
			UserId:       20000,
			FundId:       20000,
			Available:    "10000",
			PositionMode: pb.AccountPositionMode_ACCOUNT_POSITION_MODE_HEDGE_DUAL_SINGLE,
		},
		Market: &pb.GetMarketResponse{
			Market: &pb.MarketView{
				Symbol:                  "BTCUSDT",
				PriceTickTicks:          10,
				BaseIncrement:           1,
				BaseMinSize:             1,
				EffectiveQuotePrecision: 2,
				EffectiveBasePrecision:  3,
				Status:                  1,
				MaxLeverage:             150,
				MakerFeeRate:            "0",
				TakerFeeRate:            "0.0005",
			},
			Health: &pb.MarketHealth{
				Ok:             true,
				MarkPriceTicks: 7468708,
				BestBidTicks:   7468700,
				BestAskTicks:   7468710,
			},
		},
		Positions: []*pb.PositionRow{{
			UserId:            20000,
			FundId:            20000,
			Symbol:            "BTCUSDT",
			Size:              "0.001",
			EntryPrice:        "74687.1",
			Leverage:          "10",
			PositionType:      pb.PositionType_POSITION_TYPE_CROSS,
			PositionDirection: pb.PositionDirection_POSITION_DIRECTION_LONG,
			PositionId:        "actual-long-position",
		}},
		StartedAt: time.Now(),
		ReadAt:    time.Now(),
	}
	if _, err := Normalize(input, scope); err == nil {
		t.Fatal("legacy read unexpectedly accepted hedge mode")
	}
	snapshot, err := normalizeSDK(input, scope, true)
	if err != nil || snapshot.Positions[0].PositionId != "actual-long-position" || snapshot.Positions[0].SignedQuantity != "0.001" || snapshot.Market.BestAsk != "74687.1" {
		t.Fatalf("hedge translation: %v, %v", snapshot, err)
	}
}
