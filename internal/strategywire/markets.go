package strategywire

import (
	"time"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// MarketProto maps explicit business fields without exposing publisher identity.
func MarketProto(value domain.AdminMarket) (*strategyv1.MarketMonitor, error) {
	f, i := value.Feed, value.Instrument
	if f.Validate() != nil || !domain.FixedDecimal(i.Rules.PriceTick, 20, 18, false) ||
		!domain.FixedDecimal(i.Rules.QuantityStep, 20, 18, false) || !domain.FixedDecimal(i.Rules.MinNotional, 20, 18, false) {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.MarketMonitor{
		Feed: &strategyv1.CatalogMarketFeed{
			Id:             f.ID,
			Source:         string(f.Source),
			InstrumentId:   f.InstrumentID,
			ExternalSymbol: f.Symbol,
			Mode:           string(f.Mode),
			SourceVersion:  f.SourceVersion,
			Policy: &commonv1.MarketPolicy{
				MaximumAgeMs:     f.Policy.MaximumAge.Milliseconds(),
				MaximumSkewMs:    f.Policy.MaximumSkew.Milliseconds(),
				MaximumSpreadBps: f.Policy.MaximumSpreadBP.String(),
				MaximumJumpBps:   f.Policy.MaximumJumpBP.String(),
				RecoverySamples:  int32(f.Policy.RecoverySamples),
				RecoveryWindowMs: f.Policy.RecoveryWindow.Milliseconds(),
			},
		},
		Instrument: &strategyv1.CatalogInstrument{
			Id:              i.Rules.ID,
			ExecutionVenue:  i.ExecutionVenue,
			Symbol:          i.Symbol,
			BaseAsset:       i.BaseAsset,
			QuoteAsset:      i.QuoteAsset,
			PriceTick:       i.Rules.PriceTick.String(),
			QuantityStep:    i.Rules.QuantityStep.String(),
			MinNotional:     i.Rules.MinNotional.String(),
			Status:          i.Rules.Status,
			MetadataVersion: i.Rules.MetadataVersion,
		},
		EvaluatedAtMs: monitorMillis(value.EvaluatedAt),
	}
	if value.Snapshot != nil {
		var err error
		reply.Snapshot, err = monitorSnapshotProto(value)
		if err != nil {
			return nil, err
		}
	}
	if !ValidMarket(reply) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func monitorSnapshotProto(value domain.AdminMarket) (*strategyv1.MarketMonitorSnapshot, error) {
	s := value.Snapshot
	r := s.Reading
	if r.Validate() != nil || r.Source != value.Feed.Source || r.InstrumentID != value.Feed.InstrumentID ||
		r.Symbol != value.Feed.Symbol || r.Mode != value.Feed.Mode || r.SourceVersion != value.Feed.SourceVersion {
		return nil, domain.ErrAdminUnavailable
	}
	return &strategyv1.MarketMonitorSnapshot{
		Revision:              s.Revision,
		PublisherEpoch:        s.PublisherEpoch,
		PublisherCurrent:      s.PublisherCurrent,
		PublisherExpiresAtMs:  monitorMillis(s.PublisherExpiresAt),
		PublishedAtMs:         monitorMillis(s.PublishedAt),
		PublishedValidUntilMs: monitorMillis(s.PublishedValidUntil),
		PublishedQuality:      string(s.PublishedQuality),
		PublishedReason:       string(s.PublishedReason),
		Reading: &strategyv1.MarketMonitorReading{
			Connection:    r.Connection,
			Generation:    r.Generation,
			Sequence:      r.Sequence,
			Quality:       string(r.Quality),
			Reason:        string(r.Reason),
			BidPrice:      r.Bid.String(),
			BidQuantity:   r.BidQuantity.String(),
			AskPrice:      r.Ask.String(),
			AskQuantity:   r.AskQuantity.String(),
			MidPrice:      r.Mid.String(),
			EventTimeMs:   monitorMillis(r.EventTime),
			ReceivedAtMs:  monitorMillis(r.ReceivedAt),
			EvaluatedAtMs: monitorMillis(r.EvaluatedAt),
			ValidUntilMs:  monitorMillis(r.ValidUntil),
		},
	}, nil
}

// ValidMarketPage binds every observation and cursor to the original filter.
func ValidMarketPage(value *strategyv1.ListMarketsReply, filter domain.AdminMarketFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || len(value.Markets) > filter.Page.Limit || value.NextAfterId < 0 {
		return false
	}
	previous := filter.Page.AfterID
	for _, market := range value.Markets {
		if !ValidMarket(market) || market.Feed.Id <= previous || market.Feed.Mode != string(filter.Mode) ||
			filter.Source != domain.MarketSourceUnspecified && market.Feed.Source != string(filter.Source) ||
			filter.InstrumentID != 0 && market.Feed.InstrumentId != filter.InstrumentID {
			return false
		}
		previous = market.Feed.Id
	}
	return value.NextAfterId == 0 || len(value.Markets) == filter.Page.Limit && value.NextAfterId == previous
}

// ValidMarket rejects malformed identities, nested unknowns and false health.
func ValidMarket(value *strategyv1.MarketMonitor) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || value.EvaluatedAtMs <= 0 || !validMonitorInstrument(value.Instrument) {
		return false
	}
	feed, ok := monitorFeed(value.Feed)
	if !ok || feed.InstrumentID != value.Instrument.Id {
		return false
	}
	expected := value.Instrument.BaseAsset + value.Instrument.QuoteAsset
	if feed.Source == domain.MarketSourceOKX {
		expected = value.Instrument.BaseAsset + "-" + value.Instrument.QuoteAsset
	}
	if feed.Symbol != expected {
		return false
	}
	return value.Snapshot == nil || validMonitorSnapshot(value.Snapshot, feed, time.UnixMilli(value.EvaluatedAtMs).UTC())
}

func validMonitorInstrument(i *strategyv1.CatalogInstrument) bool {
	if i == nil || len(i.ProtoReflect().GetUnknown()) != 0 || i.Id <= 0 || i.MetadataVersion <= 0 ||
		!riskText(i.ExecutionVenue, 32) || !riskText(i.Symbol, 64) || !riskText(i.BaseAsset, 32) || !riskText(i.QuoteAsset, 32) ||
		i.Status != domain.InstrumentStatusTrading && i.Status != domain.InstrumentStatusHalted {
		return false
	}
	for _, value := range []string{i.PriceTick, i.QuantityStep, i.MinNotional} {
		if _, err := domain.ParseDecimal(value, orderParameterRule(false)); err != nil {
			return false
		}
	}
	return true
}

func monitorFeed(f *strategyv1.CatalogMarketFeed) (domain.MarketFeedDefinition, bool) {
	if f == nil || len(f.ProtoReflect().GetUnknown()) != 0 || f.Id <= 0 {
		return domain.MarketFeedDefinition{}, false
	}
	policy, ok := monitorPolicy(f.Policy)
	if !ok {
		return domain.MarketFeedDefinition{}, false
	}
	feed := domain.MarketFeedDefinition{
		ID:            f.Id,
		Source:        domain.MarketSource(f.Source),
		InstrumentID:  f.InstrumentId,
		Symbol:        f.ExternalSymbol,
		Mode:          domain.MarketMode(f.Mode),
		SourceVersion: f.SourceVersion,
		Policy:        policy,
	}
	return feed, feed.Validate() == nil
}

func monitorPolicy(p *commonv1.MarketPolicy) (domain.MarketPolicy, bool) {
	if p == nil || len(p.ProtoReflect().GetUnknown()) != 0 || p.MaximumAgeMs < 1 || p.MaximumAgeMs > 5000 ||
		p.MaximumSkewMs < 0 || p.MaximumSkewMs > 1000 || p.RecoveryWindowMs < 0 || p.RecoveryWindowMs >= p.MaximumAgeMs {
		return domain.MarketPolicy{}, false
	}
	rule := domain.DecimalRule{
		IntegerDigits: 5,
		Scale:         18,
	}
	spread, spreadErr := domain.ParseDecimal(p.MaximumSpreadBps, rule)
	jump, jumpErr := domain.ParseDecimal(p.MaximumJumpBps, rule)
	if spreadErr != nil || jumpErr != nil {
		return domain.MarketPolicy{}, false
	}
	policy := domain.MarketPolicy{
		MaximumAge:      time.Duration(p.MaximumAgeMs) * time.Millisecond,
		MaximumSkew:     time.Duration(p.MaximumSkewMs) * time.Millisecond,
		MaximumSpreadBP: spread,
		MaximumJumpBP:   jump,
		RecoverySamples: int(p.RecoverySamples),
		RecoveryWindow:  time.Duration(p.RecoveryWindowMs) * time.Millisecond,
	}
	return policy, policy.Validate() == nil
}

func monitorMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}
