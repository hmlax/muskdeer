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
// The lease is locked before the snapshot, matching the publisher's order.
func validateRiskMarket(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, risk *modelv1.RiskSnapshot) (int64, error) {
	if risk.GetMarketFeedId() <= 0 || risk.MarketRevision <= 0 || risk.MarketSourceVersion <= 0 || risk.MarketMaximumAgeMs < 1 || risk.MarketMaximumAgeMs > 5000 {
		return 0, domain.ErrRiskDenied
	}
	feed, instrument, err := lockMarketProduct(ctx, tx, risk.GetMarketFeedId())
	if err != nil {
		return 0, marketRiskFailure(err)
	}
	if string(feed.Source) != risk.MarketDataSource || feed.InstrumentID != strategy.InstrumentId || string(feed.Mode) != strategy.MarketMode || feed.SourceVersion != risk.MarketSourceVersion || instrument.Status != domain.InstrumentStatusTrading {
		return 0, domain.ErrRiskDenied
	}
	var publisher modelv1.MarketPublisherLease
	if err := tx.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ? AND delete_at IS NULL", feed.ID).Clauses(clause.Locking{Strength: "SHARE"}).First(&publisher).Error; err != nil {
		return 0, marketRiskFailure(err)
	}
	var snapshot modelv1.MarketSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ? AND delete_at IS NULL", feed.ID).Clauses(clause.Locking{Strength: "SHARE"}).First(&snapshot).Error; err != nil {
		return 0, marketRiskFailure(err)
	}
	now, err := marketDatabaseTime(ctx, tx, feed.ID)
	if err != nil {
		return 0, marketRiskFailure(err)
	}
	if snapshot.PublisherOwnerId != publisher.OwnerId || snapshot.PublisherEpoch != publisher.Epoch || snapshot.PublisherEpoch != risk.MarketEpoch || snapshot.Revision < risk.MarketRevision || publisher.ExpiresAt <= now.UnixMilli() {
		return 0, domain.ErrRiskDenied
	}
	publication, err := marketPublicationValue(feed, &snapshot, publisher.ExpiresAt)
	if err != nil {
		return 0, marketRiskFailure(err)
	}
	if publication.Reading.EvaluatedAt.After(now.Add(feed.Policy.MaximumSkew)) {
		return 0, domain.ErrRiskDenied
	}
	reading := checkMarketQuality(feed, instrument, publication.Reading, publication.Baseline.Mid, now, publisher.ExpiresAt)
	if err := reading.Selected(feed.Source, feed.InstrumentID, feed.Symbol, feed.Mode, now, time.Duration(risk.MarketMaximumAgeMs)*time.Millisecond); err != nil {
		return 0, domain.ErrRiskDenied
	}
	if risk.ExpiresAt <= now.UnixMilli() || now.UnixMilli()-risk.ObservedAt > domain.MaxRiskSnapshotAge.Milliseconds() {
		return 0, domain.ErrRiskDenied
	}
	return now.UnixMilli(), nil
}

func marketRiskFailure(err error) error {
	return &corePersistenceError{
		category: domain.ErrRiskDenied,
		cause:    err,
	}
}
