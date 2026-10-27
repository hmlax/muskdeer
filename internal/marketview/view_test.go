package marketview

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestReferenceWireKeepsExactPricesAndRejectsTamperedFields(t *testing.T) {
	input := referenceFixture()
	view, err := ToProto(input)
	if err != nil || view.MidPrice != "99.9999999999999999995" {
		t.Fatalf("wire rounded mid: %v, %v", view, err)
	}
	output, err := FromProto(view)
	if err != nil || !output.Reading.Mid.Equal(input.Reading.Mid) || output.OwnerEpoch != input.OwnerEpoch || output.Reading.Mode != domain.MarketModeReplay {
		t.Fatalf("reference round trip: %+v, %v", output, err)
	}
	for _, mutate := range []func(*marketv1.MarketReference){
		func(value *marketv1.MarketReference) { value.MidPrice = "1e2" },
		func(value *marketv1.MarketReference) { value.BidPrice = "NaN" },
		func(value *marketv1.MarketReference) { value.MidPrice = "99" },
		func(value *marketv1.MarketReference) { value.SourceVersion++ },
		func(value *marketv1.MarketReference) { value.PublisherOwnerId = "reused-node-name" },
		func(value *marketv1.MarketReference) { value.AuthorityUntilMs = 0 },
		func(value *marketv1.MarketReference) { value.ExternalSymbol = "BTCUSDT?other=1" },
		func(value *marketv1.MarketReference) { value.EventTimeMs = -1 },
		func(value *marketv1.MarketReference) { value.Quality = "other" },
		func(value *marketv1.MarketReference) { value.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		candidate := proto.Clone(view).(*marketv1.MarketReference)
		mutate(candidate)
		if _, err := FromProto(candidate); err == nil {
			t.Fatal("unsafe market reference decoded")
		}
	}
}

func TestUnhealthyReferenceHasNoPriceOrBaselineRecoveryCapability(t *testing.T) {
	input := referenceFixture()
	input.AuthorityUntil = time.Time{}
	input.Reading.Quality, input.Reading.Reason = domain.MarketQualityUnavailable, domain.MarketReasonDisconnected
	input.Reading.Bid, input.Reading.Ask, input.Reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	input.Reading.BidQuantity, input.Reading.AskQuantity = decimal.Zero, decimal.Zero
	input.Reading.ValidUntil = time.Time{}
	view, err := ToProto(input)
	if err != nil || view.MidPrice != "0" || view.AuthorityUntilMs != 0 || view.ValidUntilMs != 0 {
		t.Fatalf("unhealthy reference encoding: %v, %v", view, err)
	}
	if _, err := FromProto(view); err != nil {
		t.Fatal(err)
	}
	view.MidPrice = "100"
	if _, err := FromProto(view); err == nil {
		t.Fatal("unhealthy quality hid an executable reference")
	}
	if view.ProtoReflect().Descriptor().Fields().ByName("baseline_mid") != nil {
		t.Fatal("baseline recovery was exposed in the consumer protocol")
	}
}

func referenceFixture() domain.MarketReference {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return domain.MarketReference{
		FeedID:         1,
		OwnerID:        "market.01234567890123456789012345678901",
		OwnerEpoch:     1,
		AuthorityUntil: now.Add(5 * time.Second),
		Revision:       3,
		Reading: domain.MarketReading{
			Source:        domain.MarketSourceBinance,
			InstrumentID:  1,
			Symbol:        "BTCUSDT",
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Connection:    1,
			Generation:    1,
			Sequence:      3,
			Quality:       domain.MarketQualityHealthy,
			Reason:        domain.MarketReasonNone,
			Bid:           decimal.RequireFromString("99.999999999999999999"),
			BidQuantity:   decimal.NewFromInt(1),
			Ask:           decimal.NewFromInt(100),
			AskQuantity:   decimal.NewFromInt(1),
			Mid:           decimal.RequireFromString("99.9999999999999999995"),
			EventTime:     now.Add(-time.Millisecond),
			ReceivedAt:    now,
			EvaluatedAt:   now,
			ValidUntil:    now.Add(time.Second),
		},
	}
}
