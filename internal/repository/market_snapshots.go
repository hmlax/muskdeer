package repository

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type marketSnapshots struct{ db *gorm.DB }

type marketPublicationError struct {
	class error
	cause error
}

func (e *marketPublicationError) Error() string { return e.class.Error() }
func (e *marketPublicationError) Unwrap() error { return errors.Join(e.class, e.cause) }

// NewMarketSnapshots borrows a database; it grants no authority or resources.
func NewMarketSnapshots(db *gorm.DB) (domain.MarketRepository, error) {
	if db == nil {
		return nil, domain.ErrMarketIdentity
	}
	return &marketSnapshots{db: db}, nil
}

func (r *marketSnapshots) Publish(ctx context.Context, authority domain.LeaseToken, revision int64, reading domain.MarketReading) (domain.MarketPublication, error) {
	if err := validateMarketPublication(ctx, authority, revision, reading); err != nil {
		return domain.MarketPublication{}, err
	}
	var result domain.MarketPublication
	err := r.db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Transaction(func(tx *gorm.DB) error {
		feed, instrument, err := lockMarketProduct(ctx, tx, authority.Key.MarketFeedID)
		if err != nil {
			return err
		}
		if reading.Source != feed.Source || reading.InstrumentID != feed.InstrumentID || reading.Symbol != feed.Symbol || reading.Mode != feed.Mode || reading.SourceVersion != feed.SourceVersion {
			return domain.ErrMarketIdentity
		}
		lease, nowMS, err := lockLease(ctx, tx, authority.Key)
		if err != nil {
			return err
		}
		if lease.GetOwnerId() != authority.OwnerID || lease.GetEpoch() != authority.Epoch || lease.GetExpiresAt() <= nowMS {
			return domain.ErrLeaseLost
		}
		var previous modelv1.MarketSnapshot
		err = tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ? AND delete_at IS NULL", feed.ID).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if previous.Revision != revision {
			return domain.ErrMarketRevision
		}
		if err := marketOrder(&previous, authority, reading); err != nil {
			return err
		}
		now := time.UnixMilli(nowMS).UTC()
		row, err := marketSnapshotModel(feed, instrument, &previous, authority, reading, now, lease.GetExpiresAt())
		if err != nil {
			return err
		}
		if previous.Id == 0 {
			err = tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Create(row).Error
		} else {
			update := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("id = ? AND revision = ?", previous.Id, revision).
				Select("PublisherOwnerId", "PublisherEpoch", "Revision", "Connection", "Generation", "Sequence", "Quality", "Reason", "BidPrice", "BidQuantity", "AskPrice", "AskQuantity", "MidPrice", "EventAt", "ReceivedAt", "EvaluatedAt", "ValidUntil", "BaselineMid", "BaselineAt").Updates(row)
			err = update.Error
			if err == nil && update.RowsAffected != 1 {
				err = domain.ErrMarketRevision
			}
		}
		if err != nil {
			return err
		}
		result, err = marketPublicationValue(feed, row, lease.GetExpiresAt())
		return err
	})
	if err != nil {
		return domain.MarketPublication{}, marketSnapshotFailure(ctx, err)
	}
	return result, nil
}

func (r *marketSnapshots) Get(ctx context.Context, feedID int64) (domain.MarketPublication, error) {
	if err := marketCall(ctx); err != nil {
		return domain.MarketPublication{}, err
	}
	if feedID <= 0 {
		return domain.MarketPublication{}, domain.ErrMarketIdentity
	}
	var result domain.MarketPublication
	err := r.db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Transaction(func(tx *gorm.DB) error {
		feed, instrument, err := lockMarketProduct(ctx, tx, feedID)
		if err != nil {
			return err
		}
		var lease modelv1.MarketPublisherLease
		err = tx.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ? AND delete_at IS NULL", feedID).
			Clauses(clause.Locking{Strength: "SHARE"}).First(&lease).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var row modelv1.MarketSnapshot
		if err := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ? AND delete_at IS NULL", feedID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrMarketMissing
			}
			return err
		}
		result, err = marketPublicationValue(feed, &row, lease.ExpiresAt)
		if err != nil {
			return err
		}
		now, err := marketDatabaseTime(ctx, tx, feedID)
		if err != nil {
			return err
		}
		if lease.OwnerId != row.PublisherOwnerId || lease.Epoch != row.PublisherEpoch || !result.AuthorityUntil.After(now) {
			result.AuthorityUntil = time.Time{}
			result.Reading = blockedMarket(result.Reading, domain.MarketQualityUnavailable, domain.MarketReasonDisconnected, now)
			return nil
		}
		result.Reading = checkMarketQuality(feed, instrument, result.Reading, result.Baseline.Mid, now, lease.ExpiresAt)
		return nil
	})
	if err != nil {
		return domain.MarketPublication{}, marketSnapshotFailure(ctx, err)
	}
	return result, nil
}

func lockMarketProduct(ctx context.Context, tx *gorm.DB, feedID int64) (domain.MarketFeedDefinition, *modelv1.Instrument, error) {
	var row modelv1.MarketFeed
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ? AND delete_at IS NULL", feedID).First(&row).Error; err != nil {
		return domain.MarketFeedDefinition{}, nil, err
	}
	feed, err := marketFeedValue(&row)
	if err != nil {
		return domain.MarketFeedDefinition{}, nil, err
	}
	var instrument modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", feed.InstrumentID).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&instrument).Error; err != nil {
		return domain.MarketFeedDefinition{}, nil, err
	}
	return feed, &instrument, nil
}

func marketDatabaseTime(ctx context.Context, tx *gorm.DB, feedID int64) (time.Time, error) {
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ?", feedID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(clock.NowMS).UTC(), nil
}

func validateMarketPublication(ctx context.Context, token domain.LeaseToken, revision int64, reading domain.MarketReading) error {
	if err := marketCall(ctx); err != nil {
		return err
	}
	if token.Key.Kind != domain.LeaseKindMarket || token.Key.MarketFeedID <= 0 || token.Key.AccountID != "" || token.Key.StrategyID != 0 || !ownerid.Valid(token.OwnerID) || token.Epoch <= 0 || revision < 0 || revision == math.MaxInt64 {
		return domain.ErrInvalidLease
	}
	return reading.Validate()
}

func marketOrder(previous *modelv1.MarketSnapshot, token domain.LeaseToken, reading domain.MarketReading) error {
	if previous.Id == 0 || previous.PublisherEpoch < token.Epoch {
		return nil
	}
	if previous.PublisherEpoch != token.Epoch || previous.PublisherOwnerId != token.OwnerID || reading.Connection < previous.Connection ||
		(reading.Connection == previous.Connection && (reading.Generation < previous.Generation || reading.Generation == previous.Generation && reading.Sequence < previous.Sequence)) {
		return domain.ErrOldMarketReading
	}
	return nil
}

func marketSnapshotFailure(ctx context.Context, err error) error {
	if errors.Is(err, domain.ErrLeaseLost) || errors.Is(err, domain.ErrMarketRevision) || errors.Is(err, domain.ErrMarketMissing) || errors.Is(err, domain.ErrOldMarketReading) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		var class error
		switch databaseError.ConstraintName {
		case "market_snapshot_fence":
			class = domain.ErrLeaseLost
		case "market_snapshot_quality":
			class = domain.ErrUnreliableMarket
		case "market_snapshot_sequence":
			class = domain.ErrOldMarketReading
		}
		if class != nil {
			return &marketPublicationError{
				class: class,
				cause: err,
			}
		}
	}
	return marketFailure(ctx, err)
}

func blockedMarket(reading domain.MarketReading, quality domain.MarketQuality, reason domain.MarketReason, now time.Time) domain.MarketReading {
	reading.Quality, reading.Reason = quality, reason
	reading.Bid, reading.Ask, reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	reading.BidQuantity, reading.AskQuantity = decimal.Zero, decimal.Zero
	reading.EvaluatedAt, reading.ValidUntil = now, time.Time{}
	return reading
}
