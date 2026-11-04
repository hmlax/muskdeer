package native

import (
	"testing"
	"time"

	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"google.golang.org/protobuf/proto"
)

func TestSyntheticLegacyInputCannotPublishInTradeMode(t *testing.T) {
	h, book := syntheticFixture()
	h.QuantityFraction = "0.1"
	h.SyntheticTape.Mode = configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
	if _, err := SyntheticPlan(h, "BTCUSDT", 0, book, nil); err == nil {
		t.Fatal("SyntheticPlan(aggTrade mode without source trade) accepted legacy book-only input")
	}
	h.SyntheticTape.Mode = configv1.SyntheticTapeMode(2)
	if ValidateSyntheticConfig(h) == nil {
		t.Fatal("unknown synthetic mode was accepted")
	}
}

func TestSyntheticTradePlanUsesSourceDirectionPriceQuantityAndKeepsProvenance(t *testing.T) {
	h, book := syntheticFixture()
	h.QuantityFraction = "0.1"
	h.SyntheticTape.Mode = configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
	now := time.Now()
	trade := &commonv1.HimalayaTrade{
		Symbol:       "BTCUSDT",
		Source:       Source,
		Segment:      Segment,
		EventId:      "42",
		Side:         string(domain.SideSell),
		Price:        "100.43",
		Quantity:     "0.07",
		EventAtMs:    now.UnixMilli(),
		TradeAtMs:    now.UnixMilli(),
		ReceivedAtMs: now.UnixMilli(),
		ValidUntilMs: now.Add(TradeAge).UnixMilli(),
		Publisher: &commonv1.HimalayaAuthority{
			AccountId:   TradeKey("BTCUSDT"),
			OwnerId:     "market-test",
			Epoch:       1,
			ExpiresAtMs: now.Add(10 * time.Second).UnixMilli(),
		},
	}
	input := &commonv1.SyntheticTapePlan{
		RunId:       h.SyntheticTape.RunId,
		Symbol:      "BTCUSDT",
		Book:        book,
		SourceTrade: trade,
	}
	plan, err := SyntheticSourcePlan(h, input)
	if err != nil || plan.GetSide() != string(domain.SideSell) || plan.GetPrice() != "100.5" || plan.GetQuantity() != "0.007" || plan.GetSourceTrade().GetEventId() != "42" {
		t.Fatalf("SyntheticSourcePlan(aggregate sell) = %v, %v, want SELL/100.5/0.007 from event 42", plan, err)
	}
	encoded, err := proto.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var decoded commonv1.SyntheticTapePlan
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(plan, &decoded) {
		t.Fatalf("aggregate plan lost typed provenance during protobuf roundtrip: %v", err)
	}
	trade.Price = "101"
	if plan.SourceTrade.Price != "100.43" {
		t.Fatal("source observation was borrowed instead of cloned")
	}
	trade.Side, trade.Quantity = string(domain.SideBuy), "0.3"
	target, err := SyntheticTradeTarget(h, trade)
	if err != nil || target.GetQuantity() != "0.01" {
		t.Fatalf("aggregate quantity cap = %v, %v, want 0.01", target, err)
	}
	trade.Quantity = "0.00001"
	if _, err := SyntheticTradeTarget(h, trade); err == nil {
		t.Fatal("below-lot aggregate quantity was manufactured into a publication")
	}
	trade.Quantity = "0.3"
	for _, change := range []func(*commonv1.HimalayaTrade){
		func(t *commonv1.HimalayaTrade) { t.ValidUntilMs = now.Add(-time.Second).UnixMilli() },
		func(t *commonv1.HimalayaTrade) { t.Symbol = "ETHUSDT" },
		func(t *commonv1.HimalayaTrade) { t.EventId = "042" },
		func(t *commonv1.HimalayaTrade) { t.Side = "" },
		func(t *commonv1.HimalayaTrade) { t.Publisher.AccountId = MarketKey(t.Symbol) },
		func(t *commonv1.HimalayaTrade) { t.TradeAtMs = now.Add(-2 * time.Second).UnixMilli() },
	} {
		candidate := proto.CloneOf(input)
		change(candidate.SourceTrade)
		if _, err := SyntheticSourcePlan(h, candidate); err == nil {
			t.Fatal("invalid aggregate source was admitted")
		}
	}
}
