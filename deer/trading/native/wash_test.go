package native

import (
	"testing"
	"time"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"google.golang.org/protobuf/proto"
)

func TestWashTargetUsesFreshTradeWithoutBatchOrLimits(t *testing.T) {
	policy, _ := syntheticFixture()
	policy.QuantityFraction = "0.1"
	policy.SyntheticTape.Mode = config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
	policy.SyntheticTape.RunId = ""
	policy.SyntheticTape.MaximumEvents = 1
	policy.SyntheticTape.Profiles[0].MaximumNotional = "0.0001"
	now := time.Now().UnixMilli()
	input := &execution.WashTradeRequest{
		Symbol: "BTCUSDT",
		Source: &execution.WashTradeRequest_Trade{Trade: &common.VenueTrade{
			Symbol: "BTCUSDT", Source: Source, Segment: Segment, EventId: "42",
			Side: string(domain.SideSell), Price: "100.43", Quantity: "0.079",
			EventAtMs: now, TradeAtMs: now, ReceivedAtMs: now, ValidUntilMs: now + TradeAge.Milliseconds(),
			Publisher: &common.VenueAuthority{AccountId: TradeKey("BTCUSDT")},
		}},
	}
	target, err := WashTarget(policy, input)
	if err != nil || target.GetPrice() != "100.5" || target.GetQuantity() != "0.007" || target.GetSide() != string(domain.SideSell) {
		t.Fatalf("current trade calculation = %v, %v", target, err)
	}
	for name, change := range map[string]func(*execution.WashTradeRequest){
		"below lot":       func(v *execution.WashTradeRequest) { v.GetTrade().Quantity = "0.00001" },
		"stale":           func(v *execution.WashTradeRequest) { v.GetTrade().ValidUntilMs = now - 1 },
		"wrong symbol":    func(v *execution.WashTradeRequest) { v.Symbol = "ETHUSDT" },
		"wrong source":    func(v *execution.WashTradeRequest) { v.GetTrade().Source = "unselected" },
		"wrong publisher": func(v *execution.WashTradeRequest) { v.GetTrade().Publisher.AccountId = MarketKey("BTCUSDT") },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := proto.CloneOf(input)
			change(candidate)
			if _, err := WashTarget(policy, candidate); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	policy.SyntheticTape.DevelopmentOnly = false
	if _, err := WashTarget(policy, input); err == nil {
		t.Fatal("non-development publication accepted")
	}
}

func TestWashBookTargetAlternatesAndEnforcesLot(t *testing.T) {
	policy, book := syntheticFixture()
	policy.SyntheticTape.Mode = config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK
	policy.SyntheticTape.RunId = ""
	book.Publisher = &common.VenueAuthority{AccountId: MarketKey(book.Symbol)}
	input := &execution.WashTradeRequest{
		Symbol: book.Symbol, Side: string(domain.SideBuy), Source: &execution.WashTradeRequest_Book{Book: book},
	}
	target, err := WashTarget(policy, input)
	if err != nil || target.GetPrice() != "100.2" || target.GetQuantity() != "0.01" {
		t.Fatalf("buy = %v, %v", target, err)
	}
	input.Side = string(domain.SideSell)
	target, err = WashTarget(policy, input)
	if err != nil || target.GetPrice() != "99.9" {
		t.Fatalf("sell = %v, %v", target, err)
	}
	policy.SyntheticTape.Profiles[0].Quantity = "0.0001"
	if _, err := WashTarget(policy, input); err == nil {
		t.Fatal("off-lot quantity accepted")
	}
}
