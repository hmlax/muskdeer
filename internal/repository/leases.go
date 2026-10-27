// Package repository implements domain persistence interfaces with generated
// Proto models. Callers borrow the database owned by their Deer instance.
package repository

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type leaseStore struct{ db *gorm.DB }

type leaseRecord interface {
	GetId() int64
	GetOwnerId() string
	GetEpoch() int64
	GetExpiresAt() int64
}

type leaseOperationError struct{ cause error }

func (*leaseOperationError) Error() string   { return "lease persistence operation failed" }
func (e *leaseOperationError) Unwrap() error { return e.cause }

// NewLeases borrows a configured database; it opens no resource or worker.
func NewLeases(db *gorm.DB) (domain.LeaseRepository, error) {
	if db == nil {
		return nil, domain.ErrInvalidLease
	}
	return &leaseStore{db: db}, nil
}

func (r *leaseStore) Acquire(ctx context.Context, key domain.LeaseKey, owner string, ttl time.Duration) (domain.LeaseToken, error) {
	if err := validateLeaseCall(ctx, key, owner, ttl); err != nil {
		return domain.LeaseToken{}, err
	}
	var result domain.LeaseToken
	err := r.db.WithContext(ctx).Model(leaseModel(key)).Transaction(func(tx *gorm.DB) error {
		initial := leaseModel(key)
		if err := tx.WithContext(ctx).Model(leaseModel(key)).Clauses(clause.OnConflict{DoNothing: true}).Create(initial).Error; err != nil {
			return err
		}
		current, now, err := lockLease(ctx, tx, key)
		if err != nil {
			return err
		}
		if current.GetExpiresAt() > now {
			if current.GetOwnerId() != owner {
				return domain.ErrLeaseHeld
			}
			result = leaseToken(key, current)
			return nil
		}
		if current.GetEpoch() == math.MaxInt64 {
			return domain.ErrEpochExhausted
		}
		setLease(current, owner, current.GetEpoch()+1, now+ttl.Milliseconds())
		if err := saveLease(ctx, tx, key, current); err != nil {
			return err
		}
		result = leaseToken(key, current)
		return nil
	})
	if err != nil {
		return domain.LeaseToken{}, leaseFailure(ctx, err)
	}
	return result, nil
}

func (r *leaseStore) Renew(ctx context.Context, token domain.LeaseToken, ttl time.Duration) (domain.LeaseToken, error) {
	if err := validateLeaseCall(ctx, token.Key, token.OwnerID, ttl); err != nil {
		return domain.LeaseToken{}, err
	}
	if token.Epoch <= 0 {
		return domain.LeaseToken{}, domain.ErrInvalidLease
	}
	var result domain.LeaseToken
	err := r.db.WithContext(ctx).Model(leaseModel(token.Key)).Transaction(func(tx *gorm.DB) error {
		row, now, err := lockLease(ctx, tx, token.Key)
		if err != nil {
			return err
		}
		if row.GetOwnerId() != token.OwnerID || row.GetEpoch() != token.Epoch || row.GetExpiresAt() <= now {
			return domain.ErrLeaseLost
		}
		setLease(row, token.OwnerID, token.Epoch, now+ttl.Milliseconds())
		if err := saveLease(ctx, tx, token.Key, row); err != nil {
			return err
		}
		result = leaseToken(token.Key, row)
		return nil
	})
	if err != nil {
		return domain.LeaseToken{}, leaseFailure(ctx, err)
	}
	return result, nil
}

func (r *leaseStore) Release(ctx context.Context, token domain.LeaseToken) error {
	if err := validateLeaseCall(ctx, token.Key, token.OwnerID, domain.MinLeaseTTL); err != nil {
		return err
	}
	if token.Epoch <= 0 {
		return domain.ErrInvalidLease
	}
	err := r.db.WithContext(ctx).Model(leaseModel(token.Key)).Transaction(func(tx *gorm.DB) error {
		row, _, err := lockLease(ctx, tx, token.Key)
		if err != nil {
			return err
		}
		if row.GetOwnerId() != token.OwnerID || row.GetEpoch() != token.Epoch {
			return domain.ErrLeaseLost
		}
		setLease(row, token.OwnerID, token.Epoch, 0)
		return saveLease(ctx, tx, token.Key, row)
	})
	return leaseFailure(ctx, err)
}

func lockLease(ctx context.Context, tx *gorm.DB, key domain.LeaseKey) (leaseRecord, int64, error) {
	row := leaseModel(key)
	if err := tx.WithContext(ctx).Model(leaseModel(key)).Where(leasePredicate(key)).Where("delete_at IS NULL").
		Clauses(clause.Locking{Strength: "UPDATE"}).First(row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, domain.ErrLeaseLost
		}
		return nil, 0, err
	}
	// An explicit generated Model scopes this clock-only projection to the
	// locked resource row. No client clock or floating-point value is used.
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(leaseModel(key)).Where(leasePredicate(key)).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return nil, 0, err
	}
	return row, clock.NowMS, nil
}

func saveLease(ctx context.Context, tx *gorm.DB, key domain.LeaseKey, row leaseRecord) error {
	result := tx.WithContext(ctx).Model(leaseModel(key)).Where("id = ?", row.GetId()).
		Select("OwnerId", "Epoch", "ExpiresAt").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrLeaseLost
	}
	return nil
}

func leaseModel(key domain.LeaseKey) leaseRecord {
	if key.Kind == domain.LeaseKindStrategy {
		return &modelv1.StrategyLease{StrategyInstanceId: key.StrategyID}
	}
	if key.Kind == domain.LeaseKindMarket {
		return &modelv1.MarketPublisherLease{MarketFeedId: key.MarketFeedID}
	}
	return &modelv1.ExecutionShardLease{AccountId: key.AccountID}
}

func leasePredicate(key domain.LeaseKey) clause.Expression {
	if key.Kind == domain.LeaseKindStrategy {
		return clause.Eq{
			Column: clause.Column{Name: "strategy_instance_id"},
			Value:  key.StrategyID,
		}
	}
	if key.Kind == domain.LeaseKindMarket {
		return clause.Eq{
			Column: clause.Column{Name: "market_feed_id"},
			Value:  key.MarketFeedID,
		}
	}
	return clause.Eq{
		Column: clause.Column{Name: "account_id"},
		Value:  key.AccountID,
	}
}

func setLease(row leaseRecord, owner string, epoch, expiresAt int64) {
	switch row := row.(type) {
	case *modelv1.StrategyLease:
		row.OwnerId = owner
		row.Epoch = epoch
		row.ExpiresAt = expiresAt
	case *modelv1.ExecutionShardLease:
		row.OwnerId = owner
		row.Epoch = epoch
		row.ExpiresAt = expiresAt
	case *modelv1.MarketPublisherLease:
		row.OwnerId = owner
		row.Epoch = epoch
		row.ExpiresAt = expiresAt
	}
}

func leaseToken(key domain.LeaseKey, row leaseRecord) domain.LeaseToken {
	return domain.LeaseToken{
		Key:       key,
		OwnerID:   row.GetOwnerId(),
		Epoch:     row.GetEpoch(),
		ExpiresAt: time.UnixMilli(row.GetExpiresAt()).UTC(),
	}
}

func validateLeaseCall(ctx context.Context, key domain.LeaseKey, owner string, ttl time.Duration) error {
	if ctx == nil || !ownerid.Valid(owner) || ttl < domain.MinLeaseTTL || ttl > domain.MaxLeaseTTL || ttl%time.Millisecond != 0 {
		return domain.ErrInvalidLease
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.ErrInvalidLease
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch key.Kind {
	case domain.LeaseKindStrategy:
		if key.StrategyID <= 0 || key.AccountID != "" || key.MarketFeedID != 0 {
			return domain.ErrInvalidLease
		}
	case domain.LeaseKindExecution:
		if key.StrategyID != 0 || key.AccountID == "" || len(key.AccountID) > 64 || key.MarketFeedID != 0 {
			return domain.ErrInvalidLease
		}
	case domain.LeaseKindMarket:
		if key.MarketFeedID <= 0 || key.AccountID != "" || key.StrategyID != 0 {
			return domain.ErrInvalidLease
		}
	default:
		return domain.ErrInvalidLease
	}
	return nil
}

func leaseFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrLeaseHeld) || errors.Is(err, domain.ErrLeaseLost) || errors.Is(err, domain.ErrEpochExhausted) {
		return err
	}
	return &leaseOperationError{cause: errors.Join(err, ctx.Err())}
}
