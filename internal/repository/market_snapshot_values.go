package repository

import (
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func marketSnapshotModel(feed domain.MarketFeedDefinition, instrument *modelv1.Instrument, previous *modelv1.MarketSnapshot, token domain.LeaseToken, reading domain.MarketReading, now time.Time, expiresAt int64) (*modelv1.MarketSnapshot, error) {
	baseline := decimal.Zero
	baselineAt := int64(0)
	if previous.Id != 0 {
		var err error
		baseline, err = domain.ParseLedgerDecimal(previous.BaselineMid, false, true)
		if err != nil {
			return nil, err
		}
		baselineAt = previous.BaselineAt
	}
	reading.EventTime, reading.ReceivedAt = marketTime(marketMillis(reading.EventTime)), marketTime(marketMillis(reading.ReceivedAt))
	reading.ValidUntil = marketTime(marketMillis(reading.ValidUntil))
	reading = checkMarketQuality(feed, instrument, reading, baseline, now, expiresAt)
	if reading.Quality == domain.MarketQualityHealthy {
		baseline, baselineAt = reading.Mid, now.UnixMilli()
	} else {
		reading.Bid, reading.Ask, reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
		reading.BidQuantity, reading.AskQuantity = decimal.Zero, decimal.Zero
	}
	return &modelv1.MarketSnapshot{
		MarketFeedId:     feed.ID,
		PublisherOwnerId: token.OwnerID,
		PublisherEpoch:   token.Epoch,
		Revision:         previous.Revision + 1,
		Connection:       reading.Connection,
		Generation:       reading.Generation,
		Sequence:         reading.Sequence,
		Quality:          string(reading.Quality),
		Reason:           string(reading.Reason),
		BidPrice:         reading.Bid.String(),
		BidQuantity:      reading.BidQuantity.String(),
		AskPrice:         reading.Ask.String(),
		AskQuantity:      reading.AskQuantity.String(),
		MidPrice:         reading.Mid.String(),
		EventAt:          marketMillis(reading.EventTime),
		ReceivedAt:       marketMillis(reading.ReceivedAt),
		EvaluatedAt:      now.UnixMilli(),
		ValidUntil:       marketMillis(reading.ValidUntil),
		BaselineMid:      baseline.String(),
		BaselineAt:       baselineAt,
	}, nil
}

func checkMarketQuality(feed domain.MarketFeedDefinition, instrument *modelv1.Instrument, reading domain.MarketReading, baseline decimal.Decimal, now time.Time, expiresAt int64) domain.MarketReading {
	reading.EvaluatedAt = now
	if reading.Quality != domain.MarketQualityHealthy {
		return reading
	}
	expected := instrument.BaseAsset + instrument.QuoteAsset
	if feed.Source == domain.MarketSourceOKX {
		expected = instrument.BaseAsset + "-" + instrument.QuoteAsset
	}
	if feed.Symbol != expected {
		return blockedMarket(reading, domain.MarketQualityInvalid, domain.MarketReasonPayload, now)
	}
	if reading.EventTime.After(now.Add(feed.Policy.MaximumSkew)) || reading.ReceivedAt.After(now.Add(feed.Policy.MaximumSkew)) {
		return blockedMarket(reading, domain.MarketQualityClock, domain.MarketReasonFuture, now)
	}
	for _, expiry := range []time.Time{
		reading.EventTime.Add(feed.Policy.MaximumAge),
		reading.ReceivedAt.Add(feed.Policy.MaximumAge),
		marketTime(expiresAt),
	} {
		if expiry.Before(reading.ValidUntil) {
			reading.ValidUntil = expiry
		}
	}
	if reading.Selected(feed.Source, feed.InstrumentID, feed.Symbol, feed.Mode, now, feed.Policy.MaximumAge) != nil {
		return blockedMarket(reading, domain.MarketQualityStale, domain.MarketReasonAge, now)
	}
	if reading.Ask.Sub(reading.Bid).Mul(decimal.NewFromInt(10000)).GreaterThan(reading.Mid.Mul(feed.Policy.MaximumSpreadBP)) {
		return blockedMarket(reading, domain.MarketQualitySpread, domain.MarketReasonSpread, now)
	}
	if !baseline.IsZero() && reading.Mid.Sub(baseline).Abs().Mul(decimal.NewFromInt(10000)).GreaterThan(baseline.Mul(feed.Policy.MaximumJumpBP)) {
		return blockedMarket(reading, domain.MarketQualityOutlier, domain.MarketReasonJump, now)
	}
	return reading
}

func marketPublicationValue(feed domain.MarketFeedDefinition, row *modelv1.MarketSnapshot, expiresAt int64) (domain.MarketPublication, error) {
	reading := domain.MarketReading{
		Source:        feed.Source,
		InstrumentID:  feed.InstrumentID,
		Symbol:        feed.Symbol,
		Mode:          feed.Mode,
		SourceVersion: feed.SourceVersion,
		Connection:    row.Connection,
		Generation:    row.Generation,
		Sequence:      row.Sequence,
		Quality:       domain.MarketQuality(row.Quality),
		Reason:        domain.MarketReason(row.Reason),
		EventTime:     marketTime(row.EventAt),
		ReceivedAt:    marketTime(row.ReceivedAt),
		EvaluatedAt:   marketTime(row.EvaluatedAt),
		ValidUntil:    marketTime(row.ValidUntil),
	}
	baseline := domain.MarketBaseline{
		Source:        feed.Source,
		InstrumentID:  feed.InstrumentID,
		Symbol:        feed.Symbol,
		Mode:          feed.Mode,
		SourceVersion: feed.SourceVersion,
		AcceptedAt:    marketTime(row.BaselineAt),
	}
	if err := marketSnapshotNumbers(row, &reading, &baseline); err != nil {
		return domain.MarketPublication{}, err
	}
	if err := reading.Validate(); err != nil {
		return domain.MarketPublication{}, err
	}
	return domain.MarketPublication{
		MarketReference: domain.MarketReference{
			FeedID:         feed.ID,
			OwnerID:        row.PublisherOwnerId,
			OwnerEpoch:     row.PublisherEpoch,
			AuthorityUntil: marketTime(expiresAt),
			Revision:       row.Revision,
			Reading:        reading,
		},
		Baseline: baseline,
	}, nil
}

func marketSnapshotNumbers(row *modelv1.MarketSnapshot, reading *domain.MarketReading, baseline *domain.MarketBaseline) error {
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
		AllowZero:     true,
	}
	for _, field := range []struct {
		value  string
		target *decimal.Decimal
	}{
		{
			value:  row.BidPrice,
			target: &reading.Bid,
		},
		{
			value:  row.BidQuantity,
			target: &reading.BidQuantity,
		},
		{
			value:  row.AskPrice,
			target: &reading.Ask,
		},
		{
			value:  row.AskQuantity,
			target: &reading.AskQuantity,
		},
	} {
		value, err := domain.ParseDecimal(field.value, rule)
		if err != nil {
			return err
		}
		*field.target = value
	}
	var err error
	reading.Mid, err = domain.ParseLedgerDecimal(row.MidPrice, false, true)
	if err != nil {
		return err
	}
	baseline.Mid, err = domain.ParseLedgerDecimal(row.BaselineMid, false, true)
	return err
}

func marketMillis(value time.Time) int64 {
	if value.IsZero() || value.UnixMilli() <= 0 {
		return 0
	}
	return value.UnixMilli()
}

func marketTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
