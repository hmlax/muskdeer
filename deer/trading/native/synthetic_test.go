package native

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestSyntheticPlanIsFiniteAndUsesExactSourceGrid(t *testing.T) {
	h, book := syntheticFixture()
	for _, sequence := range []uint32{0, 1} {
		plan, err := SyntheticPlan(h, "BTCUSDT", sequence, book, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantSide, wantPrice := string(domain.SideBuy), "100.2"
		if sequence == 1 {
			wantSide, wantPrice = string(domain.SideSell), "99.9"
		}
		if plan.Side != wantSide || plan.Price != wantPrice || plan.Quantity != "0.01" {
			t.Fatalf("SyntheticPlan(sequence=%d) = %s/%s/%s, want %s/%s/0.01", sequence, plan.Side, plan.Price, plan.Quantity, wantSide, wantPrice)
		}
		book.Asks[0].Price = "100.21"
		if plan.Book.Asks[0].Price != "100.11" {
			t.Fatal("plan book was not copied exactly")
		}
		book.Asks[0].Price = "100.11"
	}
	if _, err := SyntheticPlan(h, "BTCUSDT", h.SyntheticTape.MaximumEvents, book, nil); err == nil {
		t.Fatal("SyntheticPlan(exhausted count) accepted another slot")
	}
	book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
	if _, err := SyntheticPlan(h, "BTCUSDT", 0, book, nil); err == nil {
		t.Fatal("SyntheticPlan(stale source) accepted a slot")
	}
}

func TestSyntheticConfigRejectsUnboundedOrNonDevelopmentRuns(t *testing.T) {
	h, _ := syntheticFixture()
	if err := ValidateSyntheticConfig(h); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*configv1.SyntheticTape){
		func(s *configv1.SyntheticTape) { s.DevelopmentOnly = false },
		func(s *configv1.SyntheticTape) { s.MaximumEvents = 0 },
		func(s *configv1.SyntheticTape) { s.MaximumEvents = 1001 },
		func(s *configv1.SyntheticTape) { s.Interval = durationpb.New(time.Millisecond) },
		func(s *configv1.SyntheticTape) { s.Profiles[0].Quantity = "0.0001" },
		func(s *configv1.SyntheticTape) { s.Profiles[0].MaximumNotional = "0" },
		func(s *configv1.SyntheticTape) { s.Profiles = append(s.Profiles, proto.CloneOf(s.Profiles[0])) },
		func(s *configv1.SyntheticTape) { s.RunId = "../escape" },
	} {
		cfg := proto.CloneOf(h)
		change(cfg.SyntheticTape)
		if ValidateSyntheticConfig(cfg) == nil {
			t.Fatal("ValidateSyntheticConfig(unsafe policy) accepted")
		}
	}
}

func syntheticFixture() (*configv1.HimalayaRuntime, *commonv1.HimalayaBook) {
	return &configv1.HimalayaRuntime{
		Profiles: []*configv1.HimalayaProfile{{
			Symbol:       "BTCUSDT",
			PriceTick:    "0.1",
			QuantityStep: "0.001",
		}},
		SyntheticTape: &configv1.SyntheticTape{
			Enabled:         true,
			DevelopmentOnly: true,
			RunId:           "test-tape-1",
			Interval:        durationpb.New(time.Second),
			MaximumEvents:   2,
			Profiles: []*configv1.SyntheticTapeProfile{{
				Symbol:          "BTCUSDT",
				Quantity:        "0.01",
				MaximumNotional: "5",
			}},
		},
	}, &commonv1.HimalayaBook{
		Symbol:       "BTCUSDT",
		Source:       Source,
		Segment:      Segment,
		ValidUntilMs: time.Now().Add(BookAge).UnixMilli(),
		Bids: []*commonv1.HimalayaLevel{{
			Price:    "99.99",
			Quantity: "1",
		}},
		Asks: []*commonv1.HimalayaLevel{{
			Price:    "100.11",
			Quantity: "1",
		}},
	}
}
