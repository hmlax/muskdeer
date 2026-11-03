package repository

import (
	"context"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

// These maps are identity lookups of generated rows, never object conversions.
type adminMarketRows struct {
	instruments map[int64]*modelv1.Instrument
	snapshots   map[int64]*modelv1.MarketSnapshot
	leases      map[int64]*modelv1.MarketPublisherLease
}

func loadAdminMarketRows(ctx context.Context, tx *gorm.DB, feeds []*modelv1.MarketFeed) (adminMarketRows, error) {
	feedIDs, instrumentIDs := make([]int64, 0, len(feeds)), make([]int64, 0, len(feeds))
	for _, feed := range feeds {
		feedIDs = append(feedIDs, feed.Id)
		instrumentIDs = append(instrumentIDs, feed.InstrumentId)
	}
	var instruments []*modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id IN ? AND delete_at IS NULL", instrumentIDs).Find(&instruments).Error; err != nil {
		return adminMarketRows{}, err
	}
	var snapshots []*modelv1.MarketSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id IN ? AND delete_at IS NULL", feedIDs).Find(&snapshots).Error; err != nil {
		return adminMarketRows{}, err
	}
	var leases []*modelv1.MarketPublisherLease
	if err := tx.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id IN ? AND delete_at IS NULL", feedIDs).Find(&leases).Error; err != nil {
		return adminMarketRows{}, err
	}
	result := adminMarketRows{
		instruments: make(map[int64]*modelv1.Instrument, len(instruments)),
		snapshots:   make(map[int64]*modelv1.MarketSnapshot, len(snapshots)),
		leases:      make(map[int64]*modelv1.MarketPublisherLease, len(leases)),
	}
	for _, row := range instruments {
		result.instruments[row.Id] = row
	}
	for _, row := range snapshots {
		result.snapshots[row.MarketFeedId] = row
	}
	for _, row := range leases {
		result.leases[row.MarketFeedId] = row
	}
	return result, nil
}
