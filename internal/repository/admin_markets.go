package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminMarkets struct{ snapshots adminStrategies }

// NewAdminMarkets borrows storage without provisioning feeds or publishers.
func NewAdminMarkets(db *gorm.DB) (domain.AdminMarkets, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminMarkets{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminMarkets) List(ctx context.Context, token string, filter domain.AdminMarketFilter) (domain.AdminMarketPage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminMarketPage{}, err
	}
	result := domain.AdminMarketPage{Items: make([]domain.AdminMarket, 0, filter.Page.Limit)}
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		if filter.InstrumentID > 0 {
			var instrument modelv1.Instrument
			if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", filter.InstrumentID).Take(&instrument).Error; err != nil {
				return adminMarketNotFound(err)
			}
		}
		query := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).
			Where("id > ? AND mode = ? AND delete_at IS NULL", filter.Page.AfterID, filter.Mode)
		if filter.Source != domain.MarketSourceUnspecified {
			query = query.Where("source = ?", filter.Source)
		}
		if filter.InstrumentID > 0 {
			query = query.Where("instrument_id = ?", filter.InstrumentID)
		}
		var rows []*modelv1.MarketFeed
		if err := query.Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Page.Limit {
			rows = rows[:filter.Page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		var err error
		result.Items, err = adminMarketValues(ctx, tx, rows)
		return err
	})
	if err != nil {
		return domain.AdminMarketPage{}, err
	}
	return result, nil
}

func (s *adminMarkets) Detail(ctx context.Context, token string, id int64) (domain.AdminMarket, error) {
	if id <= 0 {
		return domain.AdminMarket{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminMarket
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.MarketFeed
		if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ? AND delete_at IS NULL", id).Take(&row).Error; err != nil {
			return adminMarketNotFound(err)
		}
		items, err := adminMarketValues(ctx, tx, []*modelv1.MarketFeed{&row})
		if err != nil {
			return err
		}
		result = items[0]
		return nil
	})
	if err != nil {
		return domain.AdminMarket{}, err
	}
	return result, nil
}

func adminMarketValues(ctx context.Context, tx *gorm.DB, feeds []*modelv1.MarketFeed) ([]domain.AdminMarket, error) {
	result := make([]domain.AdminMarket, 0, len(feeds))
	if len(feeds) == 0 {
		return result, nil
	}
	rows, err := loadAdminMarketRows(ctx, tx, feeds)
	if err != nil {
		return nil, err
	}
	now, err := marketDatabaseTime(ctx, tx, feeds[0].Id)
	if err != nil {
		return nil, err
	}
	for _, row := range feeds {
		value, err := adminMarketValue(row, rows, now)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func adminMarketValue(row *modelv1.MarketFeed, rows adminMarketRows, now time.Time) (domain.AdminMarket, error) {
	feed, err := marketFeedValue(row)
	if err != nil {
		return domain.AdminMarket{}, err
	}
	instrument := rows.instruments[feed.InstrumentID]
	if instrument == nil {
		return domain.AdminMarket{}, domain.ErrMarketIdentity
	}
	rules, err := strategyInstrumentValue(instrument)
	if err != nil {
		return domain.AdminMarket{}, err
	}
	result := domain.AdminMarket{
		Feed: feed,
		Instrument: domain.AdminInstrument{
			Rules:          rules,
			ExecutionVenue: instrument.ExecutionVenue,
			Symbol:         instrument.Symbol,
			BaseAsset:      instrument.BaseAsset,
			QuoteAsset:     instrument.QuoteAsset,
		},
		EvaluatedAt: now,
	}
	if snapshot := rows.snapshots[feed.ID]; snapshot != nil {
		result.Snapshot, err = adminMarketObservation(feed, instrument, snapshot, rows.leases[feed.ID], now)
		if err != nil {
			return domain.AdminMarket{}, err
		}
	}
	return result, nil
}

func adminMarketObservation(feed domain.MarketFeedDefinition, instrument *modelv1.Instrument, row *modelv1.MarketSnapshot, lease *modelv1.MarketPublisherLease, now time.Time) (*domain.AdminMarketSnapshot, error) {
	if row.MarketFeedId != feed.ID || row.Revision <= 0 || row.PublisherEpoch <= 0 || !ownerid.Valid(row.PublisherOwnerId) {
		return nil, domain.ErrMarketIdentity
	}
	var expiresAt int64
	if lease != nil {
		expiresAt = lease.ExpiresAt
	}
	publication, err := marketPublicationValue(feed, row, expiresAt)
	if err != nil {
		return nil, err
	}
	current := lease != nil && lease.MarketFeedId == feed.ID && lease.OwnerId == row.PublisherOwnerId && lease.Epoch == row.PublisherEpoch && expiresAt > now.UnixMilli()
	if current {
		publication.Reading = checkMarketQuality(feed, instrument, publication.Reading, publication.Baseline.Mid, now, expiresAt)
	} else {
		publication.Reading = blockedMarket(publication.Reading, domain.MarketQualityUnavailable, domain.MarketReasonDisconnected, now)
	}
	return &domain.AdminMarketSnapshot{
		Revision:            row.Revision,
		PublisherEpoch:      row.PublisherEpoch,
		PublisherCurrent:    current,
		PublisherExpiresAt:  marketTime(expiresAt),
		PublishedAt:         marketTime(row.EvaluatedAt),
		PublishedValidUntil: marketTime(row.ValidUntil),
		PublishedQuality:    domain.MarketQuality(row.Quality),
		PublishedReason:     domain.MarketReason(row.Reason),
		Reading:             publication.Reading,
	}, nil
}

func adminMarketNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrAdminNotFound
	}
	return err
}
