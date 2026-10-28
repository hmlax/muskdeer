package repository

import (
	"context"
	"database/sql"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Missing core reads alone do not release risk. This L1-only path waits for
// core admission using its strategy/instrument mutexes, closes the original
// database deadline, and verifies retained command and order history.
func (s *intentStore) resolveSimulatorSubmission(ctx context.Context, current domain.IntentSnapshot) (domain.IntentSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.IntentSnapshot{}, err
	}
	var result domain.IntentSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(func(tx *gorm.DB) error {
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
		rows, err := lockIntentState(ctx, tx, current)
		if err != nil {
			return err
		}
		row := rows.intent
		now, err := intentClock(ctx, tx, row.Id)
		if err != nil {
			return err
		}
		if (row.State != string(domain.OrderStateIntent) && row.State != string(domain.OrderStateSubmitting) && row.State != string(domain.OrderStateUnknown)) || row.SubmissionDeadlineAt <= 0 || row.SubmissionDeadlineAt > now || row.CoreOrderId != nil || row.CoreRevision != 0 {
			return domain.ErrOutcomeUnknown
		}
		for _, query := range []*gorm.DB{
			tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id = ?", row.AccountId, row.ClientOrderId),
			tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("account_id = ? AND action = ? AND (client_order_id = ? OR intent_id = ?)", row.AccountId, domain.GatewayActionSubmit, row.ClientOrderId, row.Id),
		} {
			var count int64
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return domain.ErrOutcomeUnknown
			}
		}
		before, err := decimal.NewFromString(row.ReservedQuoteNotional)
		if err != nil {
			return err
		}
		row.State, row.ErrorCode, row.ReservedQuoteNotional = string(domain.OrderStateRejected), domain.IntentErrorCoreNotAccepted, "0"
		if err := releaseIntentReservation(ctx, tx, rows, before); err != nil {
			return err
		}
		if err := saveIntentState(ctx, tx, rows, now); err != nil {
			return err
		}
		result, err = intentSnapshot(row)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}
