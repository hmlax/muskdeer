package repository

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Resolution changes only the expired command, never its parent's state,
// balances or risk. The same core mutex waits for any in-flight cancellation.
func (s *cancellationStore) resolveSimulatorCancellation(ctx context.Context, current domain.CancellationSnapshot) (domain.CancellationSnapshot, error) {
	var result domain.CancellationSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(func(tx *gorm.DB) error {
		var strategy modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&strategy, current.Input.Authority.StrategyID).Error; err != nil {
			return err
		}
		if strategy.AccountId != current.Input.AccountID || strategy.InstrumentId != current.Input.InstrumentID {
			return domain.ErrInvalidOrder
		}
		if _, err := lockCoreRows(ctx, tx, current.Input.AccountID, current.Input.InstrumentID); err != nil {
			return err
		}
		row, err := lockCancellation(ctx, tx, current)
		if err != nil {
			return err
		}
		if row.StrategyInstanceId != strategy.Id || row.InstrumentId != strategy.InstrumentId {
			return domain.ErrInvalidOrder
		}
		now, err := intentClock(ctx, tx, row.IntentId)
		if err != nil {
			return err
		}
		if !cancellationMayHaveNoEffect(domain.CancellationState(row.State)) || row.DeadlineAt <= 0 || row.DeadlineAt > now {
			return domain.ErrOutcomeUnknown
		}
		var count int64
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).
			Where("account_id = ? AND action = ? AND command_key = ?", row.AccountId, domain.GatewayActionCancel, row.CoreCommandKey).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return domain.ErrOutcomeUnknown
		}
		row.State, row.ErrorCode = string(domain.CancellationRejected), domain.IntentErrorCoreNotAccepted
		if err := saveCancellationState(ctx, tx, row, domain.EventOrderCancelStateChanged); err != nil {
			return err
		}
		result = cancellationSnapshot(row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.CancellationSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func cancellationMayHaveNoEffect(state domain.CancellationState) bool {
	switch state {
	case domain.CancellationRequested, domain.CancellationSubmitting, domain.CancellationUnknown:
		return true
	default:
		return false
	}
}
