package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type cancellationStore struct{ db *gorm.DB }

// NewCancellations borrows the database. It never sends a core command.
func NewCancellations(db *gorm.DB) (domain.CancellationRepository, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &cancellationStore{db: db}, nil
}

func (s *cancellationStore) Accept(ctx context.Context, input domain.CancelInput, execution domain.LeaseToken) (domain.CancellationSnapshot, error) {
	if err := validateCoreCommand(ctx, input.Authority, execution, input.AccountID, input.ClientOrderID, input.IdempotencyKey, input.InstrumentID, input.Deadline); err != nil {
		return domain.CancellationSnapshot{}, err
	}
	if input.RequestID != "" && !coreIdentifier(input.RequestID, 64) {
		return domain.CancellationSnapshot{}, domain.ErrInvalidOrder
	}
	var result domain.CancellationSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockAuthority(ctx, tx, input.Authority, execution, input.AccountID, input.InstrumentID); err != nil {
			return err
		}
		if _, err := lockIntentInstrument(ctx, tx, input.InstrumentID); err != nil {
			return err
		}
		var parent modelv1.OrderIntent
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND client_order_id = ? AND delete_at IS NULL", input.AccountID, input.ClientOrderID).First(&parent).Error; err != nil {
			return err
		}
		if parent.StrategyInstanceId != input.Authority.StrategyID || parent.InstrumentId != input.InstrumentID {
			return domain.ErrInvalidOrder
		}
		current, err := intentSnapshot(&parent)
		if err != nil {
			return err
		}
		rows, err := lockIntentState(ctx, tx, current)
		if err != nil {
			return err
		}
		var existing modelv1.CancellationRequest
		err = tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("account_id = ? AND idempotency_key = ?", input.AccountID, input.IdempotencyKey).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing).Error
		if err == nil {
			if existing.IntentId != parent.Id || existing.DeleteAt != nil {
				return domain.ErrIdempotencyConflict
			}
			if existing.State == string(domain.CancellationRequested) && cancellationAuthorityChanged(&existing, input, execution) {
				applyCancellationAuthority(&existing, input, execution)
				if err := saveCancellationState(ctx, tx, &existing, domain.EventOrderCancelStateChanged); err != nil {
					return err
				}
			}
			result = cancellationSnapshot(&existing)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := stageCancellation(ctx, tx, rows, input, execution)
		if err != nil {
			return err
		}
		result = cancellationSnapshot(row)
		return nil
	})
	if err != nil {
		return domain.CancellationSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func stageCancellation(ctx context.Context, tx *gorm.DB, rows intentStateRows, input domain.CancelInput, execution domain.LeaseToken) (*modelv1.CancellationRequest, error) {
	digest := sha256.Sum256([]byte(input.AccountID + "\n" + input.IdempotencyKey))
	row := &modelv1.CancellationRequest{
		AccountId:          input.AccountID,
		IdempotencyKey:     input.IdempotencyKey,
		IntentId:           rows.intent.Id,
		ClientOrderId:      input.ClientOrderID,
		StrategyInstanceId: input.Authority.StrategyID,
		InstrumentId:       input.InstrumentID,
		CoreCommandKey:     domain.CoreCancelKeyPrefix + hex.EncodeToString(digest[:]),
		State:              string(domain.CancellationRequested),
		Revision:           1,
	}
	applyCancellationAuthority(row, input, execution)
	if rows.intent.State == string(domain.OrderStateIntent) {
		before, err := decimal.NewFromString(rows.intent.ReservedQuoteNotional)
		if err != nil {
			return nil, err
		}
		rows.intent.State = string(domain.OrderStateCanceled)
		rows.intent.CancelState = string(domain.CancelStateConfirmed)
		rows.intent.ReservedQuoteNotional = "0"
		if err := releaseIntentReservation(ctx, tx, rows, before); err != nil {
			return nil, err
		}
		now, err := intentClock(ctx, tx, rows.intent.Id)
		if err != nil {
			return nil, err
		}
		if err := saveIntentState(ctx, tx, rows, now); err != nil {
			return nil, err
		}
	}
	if terminalIntent(rows.intent.State) {
		row.State = string(domain.CancellationConfirmed)
	}
	if err := tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Create(row).Error; err != nil {
		return nil, err
	}
	return row, appendCancellationEvent(ctx, tx, row, domain.EventOrderCancelRequested)
}

func (s *cancellationStore) ByID(ctx context.Context, accountID string, id int64) (domain.CancellationSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.CancellationSnapshot{}, err
	}
	if !coreIdentifier(accountID, 64) || id <= 0 {
		return domain.CancellationSnapshot{}, domain.ErrInvalidOrder
	}
	var row modelv1.CancellationRequest
	if err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("account_id = ? AND delete_at IS NULL", accountID).First(&row, id).Error; err != nil {
		return domain.CancellationSnapshot{}, coreFailure(ctx, err)
	}
	return cancellationSnapshot(&row), nil
}

func (s *cancellationStore) ByKey(ctx context.Context, accountID, idempotencyKey string) (domain.CancellationSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.CancellationSnapshot{}, err
	}
	if !coreIdentifier(accountID, 64) || !coreIdentifier(idempotencyKey, 128) {
		return domain.CancellationSnapshot{}, domain.ErrInvalidOrder
	}
	var row modelv1.CancellationRequest
	if err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).
		Where("account_id = ? AND idempotency_key = ? AND delete_at IS NULL", accountID, idempotencyKey).First(&row).Error; err != nil {
		return domain.CancellationSnapshot{}, coreFailure(ctx, err)
	}
	return cancellationSnapshot(&row), nil
}

func applyCancellationAuthority(row *modelv1.CancellationRequest, input domain.CancelInput, execution domain.LeaseToken) {
	row.ConfigVersion = input.Authority.ConfigVersion
	row.OwnerId, row.OwnerEpoch = input.Authority.OwnerID, input.Authority.OwnerEpoch
	row.ExecutionOwnerId, row.ExecutionEpoch = execution.OwnerID, execution.Epoch
	row.DeadlineAt = input.Deadline.UnixMilli()
	row.RequestId = input.RequestID
}

func cancellationAuthorityChanged(row *modelv1.CancellationRequest, input domain.CancelInput, execution domain.LeaseToken) bool {
	return row.ConfigVersion != input.Authority.ConfigVersion || row.OwnerId != input.Authority.OwnerID || row.OwnerEpoch != input.Authority.OwnerEpoch ||
		row.ExecutionOwnerId != execution.OwnerID || row.ExecutionEpoch != execution.Epoch || row.DeadlineAt != input.Deadline.UnixMilli()
}
