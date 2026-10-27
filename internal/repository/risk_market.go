package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// A cached risk flag never substitutes for the current selected publication.
func validateRiskMarket(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, risk *modelv1.RiskSnapshot) (int64, error) {
	if risk.GetMarketFeedId() <= 0 || risk.MarketRevision <= 0 || risk.MarketSourceVersion <= 0 {
		return 0, domain.ErrRiskDenied
	}
	publication, now, err := readRiskPublication(ctx, tx, strategy, risk.GetMarketFeedId(), risk.MarketMaximumAgeMs)
	if err != nil {
		return 0, err
	}
	if publication.OwnerEpoch != risk.MarketEpoch || publication.Revision < risk.MarketRevision || publication.Reading.SourceVersion != risk.MarketSourceVersion ||
		risk.ExpiresAt <= now.UnixMilli() || now.UnixMilli()-risk.ObservedAt > domain.MaxRiskSnapshotAge.Milliseconds() {
		return 0, domain.ErrRiskDenied
	}
	return now.UnixMilli(), nil
}

// The lease is locked before the snapshot, matching the publisher's order.
// Evaluation and order admission share exactly this current-data boundary.
func readRiskPublication(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, feedID, maximumAgeMS int64) (domain.MarketReference, time.Time, error) {
	if feedID <= 0 || maximumAgeMS < 1 || maximumAgeMS > 5000 || strategy.ActiveMarketDataSource == nil {
		return domain.MarketReference{}, time.Time{}, domain.ErrRiskDenied
	}
	feed, instrument, err := lockMarketProduct(ctx, tx, feedID)
	if err != nil {
		return domain.MarketReference{}, time.Time{}, marketRiskFailure(err)
	}
	if string(feed.Source) != *strategy.ActiveMarketDataSource || feed.InstrumentID != strategy.InstrumentId || string(feed.Mode) != strategy.MarketMode || instrument.Status != domain.InstrumentStatusTrading {
		return domain.MarketReference{}, time.Time{}, domain.ErrRiskDenied
	}
	var publisher modelv1.MarketPublisherLease
	if err := tx.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ? AND delete_at IS NULL", feed.ID).Clauses(clause.Locking{Strength: "SHARE"}).First(&publisher).Error; err != nil {
		return domain.MarketReference{}, time.Time{}, marketRiskFailure(err)
	}
	var snapshot modelv1.MarketSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ? AND delete_at IS NULL", feed.ID).Clauses(clause.Locking{Strength: "SHARE"}).First(&snapshot).Error; err != nil {
		return domain.MarketReference{}, time.Time{}, marketRiskFailure(err)
	}
	now, err := marketDatabaseTime(ctx, tx, feed.ID)
	if err != nil {
		return domain.MarketReference{}, time.Time{}, marketRiskFailure(err)
	}
	if snapshot.PublisherOwnerId != publisher.OwnerId || snapshot.PublisherEpoch != publisher.Epoch || publisher.ExpiresAt <= now.UnixMilli() {
		return domain.MarketReference{}, time.Time{}, domain.ErrRiskDenied
	}
	publication, err := marketPublicationValue(feed, &snapshot, publisher.ExpiresAt)
	if err != nil {
		return domain.MarketReference{}, time.Time{}, marketRiskFailure(err)
	}
	if publication.Reading.EvaluatedAt.After(now.Add(feed.Policy.MaximumSkew)) {
		return domain.MarketReference{}, time.Time{}, domain.ErrRiskDenied
	}
	reading := checkMarketQuality(feed, instrument, publication.Reading, publication.Baseline.Mid, now, publisher.ExpiresAt)
	if err := reading.Selected(feed.Source, feed.InstrumentID, feed.Symbol, feed.Mode, now, time.Duration(maximumAgeMS)*time.Millisecond); err != nil {
		return domain.MarketReference{}, time.Time{}, domain.ErrRiskDenied
	}
	publication.Reading = reading
	return publication.MarketReference, now, nil
}

func marketRiskFailure(err error) error {
	return &corePersistenceError{
		category: domain.ErrRiskDenied,
		cause:    err,
	}
}
