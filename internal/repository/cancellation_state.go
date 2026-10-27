package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *cancellationStore) Prepare(ctx context.Context, current domain.CancellationSnapshot) (domain.CancellationSnapshot, error) {
	in := current.Input
	if err := validateCoreCommand(ctx, in.Authority, current.ExecutionAuthority, in.AccountID, in.ClientOrderID, in.IdempotencyKey, in.InstrumentID, in.Deadline); err != nil {
		return domain.CancellationSnapshot{}, err
	}
	var result domain.CancellationSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockAuthority(ctx, tx, in.Authority, current.ExecutionAuthority, in.AccountID, in.InstrumentID); err != nil {
			return err
		}
		row, err := lockCancellation(ctx, tx, current)
		if err != nil {
			return err
		}
		if row.State != string(domain.CancellationRequested) {
			return domain.ErrOutcomeUnknown
		}
		row.State = string(domain.CancellationSubmitting)
		row.ErrorCode = ""
		if err := saveCancellationState(ctx, tx, row, domain.EventOrderCancelStateChanged); err != nil {
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

func (s *cancellationStore) Observe(ctx context.Context, current domain.CancellationSnapshot, state domain.CancellationState, code string) (domain.CancellationSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.CancellationSnapshot{}, err
	}
	if state != domain.CancellationPending && state != domain.CancellationConfirmed && state != domain.CancellationUnknown && state != domain.CancellationRejected && state != domain.CancellationRequested {
		return domain.CancellationSnapshot{}, domain.ErrInvalidOrder
	}
	if code != "" && code != domain.IntentErrorOutcomeUnknown && code != domain.IntentErrorCoreNotFound && code != domain.IntentErrorCoreRejected {
		return domain.CancellationSnapshot{}, domain.ErrInvalidOrder
	}
	var result domain.CancellationSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(func(tx *gorm.DB) error {
		row, err := lockCancellation(ctx, tx, current)
		if err != nil {
			return err
		}
		if row.State == string(domain.CancellationConfirmed) || row.State == string(domain.CancellationRejected) || (row.State == string(state) && row.ErrorCode == code) {
			result = cancellationSnapshot(row)
			return nil
		}
		if state == domain.CancellationRequested && row.State != string(domain.CancellationRequested) {
			return domain.ErrInvalidOrder
		}
		row.State, row.ErrorCode = string(state), code
		if err := saveCancellationState(ctx, tx, row, domain.EventOrderCancelStateChanged); err != nil {
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

func lockCancellation(ctx context.Context, tx *gorm.DB, current domain.CancellationSnapshot) (*modelv1.CancellationRequest, error) {
	if current.ID <= 0 || current.Revision <= 0 || !coreIdentifier(current.Input.AccountID, 64) {
		return nil, domain.ErrInvalidOrder
	}
	var row modelv1.CancellationRequest
	if err := tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("account_id = ? AND delete_at IS NULL", current.Input.AccountID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, current.ID).Error; err != nil {
		return nil, err
	}
	if row.Revision != current.Revision {
		return nil, domain.ErrIdempotencyConflict
	}
	return &row, nil
}

func saveCancellationState(ctx context.Context, tx *gorm.DB, row *modelv1.CancellationRequest, kind string) error {
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("State", "ErrorCode", "ConfigVersion", "OwnerId", "OwnerEpoch", "ExecutionOwnerId", "ExecutionEpoch", "DeadlineAt", "RequestId", "Revision").Updates(row)
	if err := reservationResult(result); err != nil {
		return err
	}
	return appendCancellationEvent(ctx, tx, row, kind)
}

func cancellationSnapshot(row *modelv1.CancellationRequest) domain.CancellationSnapshot {
	return domain.CancellationSnapshot{
		ID:       row.Id,
		IntentID: row.IntentId,
		Input: domain.CancelInput{
			RequestID:      row.RequestId,
			IdempotencyKey: row.IdempotencyKey,
			Authority: domain.StrategyAuthority{
				StrategyID:    row.StrategyInstanceId,
				ConfigVersion: row.ConfigVersion,
				OwnerID:       row.OwnerId,
				OwnerEpoch:    row.OwnerEpoch,
			},
			AccountID:     row.AccountId,
			InstrumentID:  row.InstrumentId,
			ClientOrderID: row.ClientOrderId,
			Deadline:      time.UnixMilli(row.DeadlineAt).UTC(),
		},
		ExecutionAuthority: domain.LeaseToken{
			Key: domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: row.AccountId,
			},
			OwnerID: row.ExecutionOwnerId,
			Epoch:   row.ExecutionEpoch,
		},
		CoreCommandKey: row.CoreCommandKey,
		State:          domain.CancellationState(row.State),
		Revision:       row.Revision,
		ErrorCode:      row.ErrorCode,
	}
}
