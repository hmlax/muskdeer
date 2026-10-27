package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type intentStore struct{ db *gorm.DB }

// NewIntents borrows the database and does not call a trading gateway.
func NewIntents(db *gorm.DB) (domain.IntentExecutionRepository, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &intentStore{db: db}, nil
}

func (s *intentStore) Accept(ctx context.Context, in domain.OrderInput, execution domain.LeaseToken) (domain.IntentSnapshot, error) {
	if !coreIdentifier(in.AccountID, 64) || !coreIdentifier(in.IdempotencyKey, 128) {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	digest := sha256.Sum256([]byte(in.AccountID + "\n" + in.IdempotencyKey))
	clientID := hex.EncodeToString(digest[:])
	if err := validateCoreCommand(ctx, in.Authority, execution, in.AccountID, clientID, in.IdempotencyKey, in.InstrumentID, in.Deadline); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if !domain.FixedDecimal(in.Price, 20, 18, false) || !domain.FixedDecimal(in.Quantity, 20, 18, false) ||
		(in.Side != domain.SideBuy && in.Side != domain.SideSell) || (in.TimeInForce != domain.TimeInForceGTC && in.TimeInForce != domain.TimeInForcePostOnly) ||
		in.ReduceOnly && in.Side != domain.SideSell || in.RequestID != "" && !coreIdentifier(in.RequestID, 64) {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	fingerprint, err := fingerprintSubmit(in)
	if err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	var result domain.IntentSnapshot
	err = s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(func(tx *gorm.DB) error {
		strategy, _, err := lockAuthority(ctx, tx, in.Authority, execution, in.AccountID, in.InstrumentID)
		if err != nil {
			return err
		}
		instrument, err := lockIntentInstrument(ctx, tx, in.InstrumentID)
		if err != nil {
			return err
		}
		var account modelv1.AccountBudget
		if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", in.AccountID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&account).Error; err != nil {
			return err
		}
		var existing modelv1.OrderIntent
		err = tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND idempotency_key = ?", in.AccountID, in.IdempotencyKey).First(&existing).Error
		if err == nil {
			if existing.Fingerprint != fingerprint || existing.DeleteAt != nil {
				return domain.ErrIdempotencyConflict
			}
			result, err = intentSnapshot(&existing)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := stageAcceptedIntent(ctx, tx, in, execution, strategy, instrument, &account, clientID, fingerprint)
		if err != nil {
			return err
		}
		result, err = intentSnapshot(row)
		return err
	})
	if err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func (s *intentStore) ByID(ctx context.Context, accountID string, intentID int64) (domain.IntentSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if !coreIdentifier(accountID, 64) || intentID <= 0 {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	var row modelv1.OrderIntent
	if err := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND delete_at IS NULL", accountID).First(&row, intentID).Error; err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	return intentSnapshot(&row)
}

func (s *intentStore) ByClient(ctx context.Context, accountID, clientOrderID string) (domain.IntentSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if !coreIdentifier(accountID, 64) || !coreIdentifier(clientOrderID, 64) {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	var row modelv1.OrderIntent
	if err := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND client_order_id = ? AND delete_at IS NULL", accountID, clientOrderID).First(&row).Error; err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	return intentSnapshot(&row)
}
