package strategywire

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
)

func monitorFixture() domain.AdminMarket {
	now := time.UnixMilli(1700000000000).UTC()
	return domain.AdminMarket{
		Feed: domain.MarketFeedDefinition{
			ID:            9007199254740993,
			Source:        domain.MarketSourceBinance,
			InstrumentID:  9007199254740994,
			Symbol:        "BTCUSDT",
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Policy:        marketquality.DefaultPolicy(),
		},
		Instrument: domain.AdminInstrument{
			Rules: domain.StrategyInstrument{
				ID:              9007199254740994,
				MetadataVersion: 1,
				Status:          domain.InstrumentStatusTrading,
				PriceTick:       decimal.RequireFromString("0.000000000000000001"),
				QuantityStep:    decimal.RequireFromString("0.000000000000000001"),
				MinNotional:     decimal.RequireFromString("99999999999999999999.123456789012345678"),
			},
			ExecutionVenue: domain.ExecutionVenueSimulator,
			Symbol:         "BTC-USDT",
			BaseAsset:      "BTC",
			QuoteAsset:     "USDT",
		},
		EvaluatedAt: now,
		Snapshot: &domain.AdminMarketSnapshot{
			Revision:            9007199254740995,
			PublisherEpoch:      9007199254740996,
			PublisherCurrent:    true,
			PublisherExpiresAt:  now.Add(2 * time.Second),
			PublishedAt:         now.Add(-time.Millisecond),
			PublishedValidUntil: now.Add(time.Second),
			PublishedQuality:    domain.MarketQualityHealthy,
			Reading: domain.MarketReading{
				Source:        domain.MarketSourceBinance,
				InstrumentID:  9007199254740994,
				Symbol:        "BTCUSDT",
				Mode:          domain.MarketModeReplay,
				SourceVersion: domain.MarketSourceVersion,
				Connection:    1,
				Generation:    1,
				Sequence:      9007199254740997,
				Quality:       domain.MarketQualityHealthy,
				Bid:           decimal.RequireFromString("99.999999999999999999"),
				BidQuantity:   decimal.NewFromInt(2),
				Ask:           decimal.NewFromInt(100),
				AskQuantity:   decimal.NewFromInt(3),
				Mid:           decimal.RequireFromString("99.9999999999999999995"),
				EventTime:     now.Add(-time.Millisecond),
				ReceivedAt:    now.Add(-time.Millisecond),
				EvaluatedAt:   now,
				ValidUntil:    now.Add(time.Second),
			},
		},
	}
}

func TestMarketWirePreservesExactPricesAndDistinctMissingOrLostPublication(t *testing.T) {
	value := monitorFixture()
	p, err := MarketProto(value)
	if err != nil || !ValidMarket(p) || p.Snapshot.Reading.MidPrice != "99.9999999999999999995" || p.Snapshot.Reading.Sequence != 9007199254740997 || p.Instrument.MinNotional != "99999999999999999999.123456789012345678" {
		t.Fatal("market protocol lost exact numbers or identity", err)
	}
	s := value.Snapshot
	s.PublisherCurrent = false
	s.Reading.Quality, s.Reading.Reason = domain.MarketQualityUnavailable, domain.MarketReasonDisconnected
	s.Reading.Bid, s.Reading.Ask, s.Reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	s.Reading.BidQuantity, s.Reading.AskQuantity, s.Reading.ValidUntil = decimal.Zero, decimal.Zero, time.Time{}
	p, err = MarketProto(value)
	if err != nil || !ValidMarket(p) || p.Snapshot.PublishedQuality != string(domain.MarketQualityHealthy) || p.Snapshot.Reading.MidPrice != "0" || p.Snapshot.PublisherCurrent {
		t.Fatal("lost publisher was promoted to a healthy quote", err)
	}
	value.Snapshot = nil
	if p, err := MarketProto(value); err != nil || !ValidMarket(p) || p.Snapshot != nil {
		t.Fatal("missing observation was fabricated", err)
	}
}

func TestMarketWireRejectsFalseHealthForeignScopesAndMalformedDecimals(t *testing.T) {
	base, err := MarketProto(monitorFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*strategyv1.MarketMonitor){
		func(p *strategyv1.MarketMonitor) { p.Feed.Id = 0 },
		func(p *strategyv1.MarketMonitor) { p.Instrument.Id++ },
		func(p *strategyv1.MarketMonitor) { p.Feed.Source = string(domain.MarketSourceOKX) },
		func(p *strategyv1.MarketMonitor) { p.Feed.Mode = "" },
		func(p *strategyv1.MarketMonitor) { p.Instrument.MinNotional = "NaN" },
		func(p *strategyv1.MarketMonitor) { p.Feed.Policy.MaximumAgeMs = 9223372036854775807 },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.PublisherCurrent = false },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.PublisherExpiresAtMs = p.EvaluatedAtMs },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.PublishedValidUntilMs = 0 },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.Sequence = -1 },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.BidPrice = "99.9999999999999999999" },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.MidPrice = "99" },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.EvaluatedAtMs++ },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.ValidUntilMs = p.EvaluatedAtMs + 5000 },
		func(p *strategyv1.MarketMonitor) { p.Snapshot.Reading.EventTimeMs = p.EvaluatedAtMs + 1000 },
		func(p *strategyv1.MarketMonitor) {
			p.Snapshot.PublishedQuality = string(domain.MarketQualityUnavailable)
			p.Snapshot.PublishedReason = string(domain.MarketReasonDisconnected)
		},
		func(p *strategyv1.MarketMonitor) { p.Feed.Policy.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
		func(p *strategyv1.MarketMonitor) {
			p.Snapshot.Reading.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
	} {
		p := proto.CloneOf(base)
		mutate(p)
		if ValidMarket(p) {
			t.Fatal("malformed successful market observation accepted")
		}
	}
	value := monitorFixture()
	value.Snapshot.Reading.Mid = decimal.New(1, 1_000_000_000)
	if result, err := MarketProto(value); result != nil || err == nil {
		t.Fatal("unbounded decimal reached string conversion")
	}
}
