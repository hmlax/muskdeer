package native

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestWashTargetUsesVenueMinimumQuantityAndNotionalWithinSingleEventCap(t *testing.T) {
	policy, _ := syntheticFixture()
	policy.QuantityFraction = "0.1"
	policy.SyntheticTape.Mode = config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
	policy.SyntheticTape.Profiles[0].Quantity = "100"
	policy.Profiles[0].PriceTick = "0.0000001"
	policy.Profiles[0].QuantityStep = "1"
	now := time.Now().UnixMilli()
	input := &execution.WashTradeRequest{
		Symbol: "BTCUSDT",
		Source: &execution.WashTradeRequest_Trade{Trade: &common.VenueTrade{
			Symbol:       "BTCUSDT",
			Source:       Source,
			Segment:      Segment,
			Side:         string(domain.SideBuy),
			Price:        "0.3131492",
			Quantity:     "1",
			EventId:      "42",
			EventAtMs:    now,
			TradeAtMs:    now,
			ReceivedAtMs: now,
			ValidUntilMs: now + TradeAge.Milliseconds(),
			Publisher:    &common.VenueAuthority{AccountId: TradeKey("BTCUSDT")},
		}},
	}
	market := &common.VenueMarket{
		PriceTick:       "0.0000001",
		QuantityStep:    "1",
		MinimumQuantity: "1",
		MaximumQuantity: "10000000",
		MinimumNotional: "0.5",
	}
	for _, tc := range []struct{ name, price, minimum, cap, want string }{
		{name: "reported below minimum",
			price:   "0.3131492",
			minimum: "1",
			cap:     "100",
			want:    "2"},
		{name: "exact notional",
			price:   "0.5",
			minimum: "1",
			cap:     "100",
			want:    "1"},
		{name: "quantity minimum rounded up",
			price:   "1",
			minimum: "2.1",
			cap:     "100",
			want:    "3"},
		{name: "large exact lot count",
			price:   "0.0000001",
			minimum: "1",
			cap:     "5000000",
			want:    "5000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input.GetTrade().Price = tc.price
			market.MinimumQuantity = tc.minimum
			policy.SyntheticTape.Profiles[0].Quantity = tc.cap
			got, err := WashTargetForMarket(policy, input, market)
			if err != nil || got.GetQuantity() != tc.want {
				t.Fatalf("minimum target=%v err=%v want=%s", got, err, tc.want)
			}
			if input.GetTrade().Quantity != "1" || input.GetTrade().Price != tc.price {
				t.Fatal("source observation changed")
			}
		})
	}
	input.GetTrade().Price = "0.3131492"
	market.MinimumQuantity = "1"
	policy.SyntheticTape.Profiles[0].Quantity = "1"
	if _, err := WashTargetForMarket(policy, input, market); !errors.Is(err, ErrWashMinimumAboveLimit) {
		t.Fatalf("single-event cap silently exceeded: %v", err)
	}
	policy.SyntheticTape.Profiles[0].Quantity = "100"
	market.MaximumQuantity = "1"
	if _, err := WashTargetForMarket(policy, input, market); !errors.Is(err, ErrWashMinimumAboveLimit) {
		t.Fatalf("venue maximum silently exceeded: %v", err)
	}
}

func TestWashMinimumLotDivisionRetainsMoreThanDefaultDecimalPrecision(t *testing.T) {
	lots := ceilWashLots(decimal.RequireFromString("0.5"), decimal.RequireFromString("0.000000000000000003"))
	if lots.String() != "166666666666666667" {
		t.Fatalf("minimum lots rounded below venue requirement: %s", lots)
	}
}
