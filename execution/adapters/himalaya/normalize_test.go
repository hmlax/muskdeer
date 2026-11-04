package himalaya

import (
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
)

func TestSDKNormalizationUsesEffectiveUnitsAndSignedPositions(t *testing.T) {
	input := &SDKSnapshot{
		Account: &pb.GetAccountResponse{
			UserId:    10001,
			FundId:    10001,
			Available: "1000.123456789",
		},
		Market: &pb.GetMarketResponse{
			Market: &pb.MarketView{
				Symbol:                  "BTCUSDT",
				PriceTickTicks:          10,
				BaseIncrement:           1,
				BaseMinSize:             1,
				QuoteMinSize:            500000000,
				EffectiveQuotePrecision: 2,
				EffectiveBasePrecision:  3,
				Status:                  1,
				MaxLeverage:             10,
				MakerFeeRate:            "0.0002",
				TakerFeeRate:            "0.0005",
			},
			Health: &pb.MarketHealth{
				Ok:             true,
				MarkPriceTicks: 10010,
			},
		},
		Positions: []*pb.PositionRow{
			{
				UserId:            10001,
				FundId:            10001,
				Symbol:            "BTCUSDT",
				Size:              "0.003",
				EntryPrice:        "100.1",
				Leverage:          "10",
				PositionType:      pb.PositionType_POSITION_TYPE_CROSS,
				PositionDirection: pb.PositionDirection_POSITION_DIRECTION_SHORT,
			},
		},
		Orders: []*pb.OrderRow{
			{
				OrderId:          42,
				UserId:           10001,
				FundId:           10001,
				Symbol:           "BTCUSDT",
				Side:             pb.Side_SIDE_BUY,
				Price:            "100.1",
				Quantity:         7,
				ExecutedQuantity: 2,
				Status:           "PARTIALLY_FILLED",
			},
		},
		StartedAt: time.UnixMilli(1000),
		ReadAt:    time.UnixMilli(1100),
	}
	scope := Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}
	view, err := Normalize(input, scope)
	if err != nil || view.Market.PriceTick != "0.1" || view.Market.QuantityStep != "0.001" || view.Market.MinimumNotional != "5" || view.Market.WorstFeeRate != "0.0005" || view.Positions[0].SignedQuantity != "-0.003" || view.Orders[0].Quantity != "0.007" || view.Orders[0].ExecutedQuantity != "0.002" {
		t.Fatalf("SDK units were translated incorrectly: %v %v", view, err)
	}
	input.Positions[0].Symbol = "ETHUSDT"
	if _, err := Normalize(input, scope); err == nil {
		t.Fatal("another instrument escaped fund risk validation")
	}
}
