package strategywire

import (
	"time"

	"github.com/shopspring/decimal"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func validMonitorSnapshot(s *strategyv1.MarketMonitorSnapshot, feed domain.MarketFeedDefinition, now time.Time) bool {
	if s == nil || len(s.ProtoReflect().GetUnknown()) != 0 || s.Revision <= 0 || s.PublisherEpoch <= 0 ||
		s.PublisherExpiresAtMs < 0 || s.PublishedAtMs <= 0 || s.PublishedValidUntilMs < 0 ||
		!domain.ValidMarketQuality(domain.MarketQuality(s.PublishedQuality), domain.MarketReason(s.PublishedReason)) {
		return false
	}
	r, ok := monitorReading(s.Reading, feed)
	if !ok || !r.EvaluatedAt.Equal(now) || r.Validate() != nil {
		return false
	}
	if s.PublishedQuality == string(domain.MarketQualityHealthy) && s.PublishedValidUntilMs <= s.PublishedAtMs {
		return false
	}
	if !s.PublisherCurrent {
		return r.Quality == domain.MarketQualityUnavailable && r.Reason == domain.MarketReasonDisconnected && r.ValidUntil.IsZero()
	}
	if s.PublisherExpiresAtMs <= now.UnixMilli() {
		return false
	}
	if s.PublishedQuality != string(domain.MarketQualityHealthy) {
		return string(r.Quality) == s.PublishedQuality && string(r.Reason) == s.PublishedReason
	}
	if r.Quality != domain.MarketQualityHealthy {
		return r.ValidUntil.IsZero() && (r.Quality == domain.MarketQualityStale || r.Quality == domain.MarketQualityClock || r.Quality == domain.MarketQualitySpread || r.Quality == domain.MarketQualityOutlier)
	}
	return validHealthyMonitorReading(s, r, feed, now)
}

func validHealthyMonitorReading(s *strategyv1.MarketMonitorSnapshot, r domain.MarketReading, feed domain.MarketFeedDefinition, now time.Time) bool {
	return r.Selected(feed.Source, feed.InstrumentID, feed.Symbol, feed.Mode, now, feed.Policy.MaximumAge) == nil &&
		!r.EventTime.After(now.Add(feed.Policy.MaximumSkew)) && !r.ReceivedAt.After(now.Add(feed.Policy.MaximumSkew)) &&
		r.ValidUntil.UnixMilli() <= s.PublisherExpiresAtMs && r.ValidUntil.UnixMilli() <= s.PublishedValidUntilMs &&
		!r.ValidUntil.After(r.EventTime.Add(feed.Policy.MaximumAge)) && !r.ValidUntil.After(r.ReceivedAt.Add(feed.Policy.MaximumAge)) &&
		r.Ask.Sub(r.Bid).Mul(decimal.NewFromInt(10000)).LessThanOrEqual(r.Mid.Mul(feed.Policy.MaximumSpreadBP))
}

func monitorReading(p *strategyv1.MarketMonitorReading, feed domain.MarketFeedDefinition) (domain.MarketReading, bool) {
	if p == nil || len(p.ProtoReflect().GetUnknown()) != 0 || p.EventTimeMs < 0 || p.ReceivedAtMs < 0 || p.EvaluatedAtMs <= 0 || p.ValidUntilMs < 0 {
		return domain.MarketReading{}, false
	}
	r := domain.MarketReading{
		Source:        feed.Source,
		InstrumentID:  feed.InstrumentID,
		Symbol:        feed.Symbol,
		Mode:          feed.Mode,
		SourceVersion: feed.SourceVersion,
		Connection:    p.Connection,
		Generation:    p.Generation,
		Sequence:      p.Sequence,
		Quality:       domain.MarketQuality(p.Quality),
		Reason:        domain.MarketReason(p.Reason),
		EventTime:     monitorTime(p.EventTimeMs),
		ReceivedAt:    monitorTime(p.ReceivedAtMs),
		EvaluatedAt:   monitorTime(p.EvaluatedAtMs),
		ValidUntil:    monitorTime(p.ValidUntilMs),
	}
	for _, field := range []struct {
		text   string
		target *decimal.Decimal
	}{
		{
			text:   p.BidPrice,
			target: &r.Bid,
		},
		{
			text:   p.BidQuantity,
			target: &r.BidQuantity,
		},
		{
			text:   p.AskPrice,
			target: &r.Ask,
		},
		{
			text:   p.AskQuantity,
			target: &r.AskQuantity,
		},
	} {
		value, err := domain.ParseDecimal(field.text, orderParameterRule(true))
		if err != nil {
			return domain.MarketReading{}, false
		}
		*field.target = value
	}
	var err error
	r.Mid, err = domain.ParseLedgerDecimal(p.MidPrice, false, true)
	return r, err == nil
}

func monitorTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
