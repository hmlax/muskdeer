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
	"github.com/hmlax/muskdeer/internal/outbox"
)

type outboxStore struct{ db *gorm.DB }
type outboxPersistenceError struct{ cause error }

func (*outboxPersistenceError) Error() string   { return outbox.MessagePersistenceFailure }
func (e *outboxPersistenceError) Unwrap() error { return e.cause }

// NewOutbox borrows the database and provisions no broker resource.
func NewOutbox(db *gorm.DB) (outbox.Store, error) {
	if db == nil {
		return nil, outbox.ErrInvalidRecord
	}
	return &outboxStore{db: db}, nil
}

func (s *outboxStore) Claim(ctx context.Context, owner string, ttl time.Duration) (*outbox.Record, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if !ownerid.Valid(owner) || ttl < 100*time.Millisecond || ttl > time.Minute || ttl%time.Millisecond != 0 {
		return nil, outbox.ErrInvalidRecord
	}
	var result *outbox.Record
	invalid := false
	err := s.db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Transaction(func(tx *gorm.DB) error {
		now, err := businessClock(ctx, tx)
		if err != nil {
			return err
		}
		var row modelv1.OutboxEvent
		err = tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("delete_at IS NULL").
			Where("(state = ? AND next_attempt_at <= ?) OR (state = ? AND claim_expires_at <= ?)", domain.OutboxStatePending, now, domain.OutboxStatePublishing, now).
			Order("id ASC").Clauses(clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		}).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now, err = businessClock(ctx, tx)
		if err != nil {
			return err
		}
		if row.Attempts == math.MaxInt64 {
			return outbox.ErrInvalidRecord
		}
		row.Attempts++
		row.State = domain.OutboxStatePublishing
		row.ClaimedBy = owner
		row.ClaimExpiresAt = now + ttl.Milliseconds()
		row.ErrorCode = ""
		if row.Event == nil || row.Event.EventId != row.EventId || row.Event.EventType != row.EventType {
			invalid = true
			row.State = domain.OutboxStateFailed
			row.ErrorCode = outbox.CodeInvalidEvent
			row.ClaimedBy = ""
			row.ClaimExpiresAt = 0
		}
		update := tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("id = ?", row.Id).
			Select("Attempts", "State", "ClaimedBy", "ClaimExpiresAt", "ErrorCode").Updates(&row)
		if err := reservationResult(update); err != nil {
			return err
		}
		if !invalid {
			result = &outbox.Record{
				ID:        row.Id,
				Owner:     owner,
				ExpiresAt: row.ClaimExpiresAt,
				Event:     row.Event,
			}
		}
		return nil
	})
	if err != nil {
		return nil, outboxFailure(ctx, err)
	}
	if invalid {
		return nil, outbox.ErrInvalidEvent
	}
	return result, nil
}

func (s *outboxStore) Complete(ctx context.Context, record *outbox.Record) error {
	return s.finish(ctx, record, domain.OutboxStatePublished, "", 0)
}

func (s *outboxStore) Retry(ctx context.Context, record *outbox.Record, code string, delay time.Duration) error {
	if code != outbox.CodePublishFailed || delay < time.Millisecond || delay > time.Minute || delay%time.Millisecond != 0 {
		return outbox.ErrInvalidRecord
	}
	return s.finish(ctx, record, domain.OutboxStatePending, code, delay)
}

func (s *outboxStore) Fail(ctx context.Context, record *outbox.Record, code string) error {
	if code != outbox.CodeInvalidEvent {
		return outbox.ErrInvalidRecord
	}
	return s.finish(ctx, record, domain.OutboxStateFailed, code, 0)
}

func (s *outboxStore) finish(ctx context.Context, record *outbox.Record, state, code string, delay time.Duration) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	if record == nil || record.ID <= 0 || !ownerid.Valid(record.Owner) || record.ExpiresAt <= 0 {
		return outbox.ErrInvalidRecord
	}
	err := s.db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Transaction(func(tx *gorm.DB) error {
		var current modelv1.OutboxEvent
		err := tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).
			Where("id = ? AND delete_at IS NULL", record.ID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return outbox.ErrClaimLost
		}
		if err != nil {
			return err
		}
		// Read database time after waiting for the row lock. A clock sampled
		// before a blocked update cannot authorize an already expired claim.
		now, err := businessClock(ctx, tx)
		if err != nil {
			return err
		}
		if current.State != domain.OutboxStatePublishing || current.ClaimedBy != record.Owner || current.ClaimExpiresAt != record.ExpiresAt || current.ClaimExpiresAt <= now {
			return outbox.ErrClaimLost
		}
		row := &modelv1.OutboxEvent{
			State:         state,
			ErrorCode:     code,
			NextAttemptAt: now + delay.Milliseconds(),
		}
		if state == domain.OutboxStatePublished {
			row.PublishedAt = &now
		}
		result := tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).
			Where("id = ? AND delete_at IS NULL AND state = ? AND claimed_by = ? AND claim_expires_at = ? AND claim_expires_at > ?", record.ID, domain.OutboxStatePublishing, record.Owner, record.ExpiresAt, now).
			Select("State", "ErrorCode", "NextAttemptAt", "PublishedAt", "ClaimedBy", "ClaimExpiresAt").Updates(row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return outbox.ErrClaimLost
		}
		return nil
	})
	return outboxFailure(ctx, err)
}

func businessClock(ctx context.Context, tx *gorm.DB) (int64, error) {
	var clock struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ? AND delete_at IS NULL", 1).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
	return clock.NowMS, err
}

func outboxFailure(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, outbox.ErrClaimLost) || errors.Is(err, outbox.ErrInvalidRecord) {
		return err
	}
	return &outboxPersistenceError{cause: errors.Join(err, ctx.Err())}
}
