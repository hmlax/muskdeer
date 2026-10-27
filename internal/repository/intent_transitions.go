package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type intentStateRows struct {
	intent   *modelv1.OrderIntent
	account  *modelv1.AccountBudget
	strategy *modelv1.StrategyBudget
}

func (s *intentStore) Prepare(ctx context.Context, current domain.IntentSnapshot, authority domain.StrategyAuthority, execution domain.LeaseToken, deadline time.Time) (domain.IntentSnapshot, error) {
	if err := validateCoreCommand(ctx, authority, execution, current.Input.AccountID, current.ClientOrderID, current.Input.IdempotencyKey, current.Input.InstrumentID, deadline); err != nil {
		return domain.IntentSnapshot{}, err
	}
	var result domain.IntentSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(func(tx *gorm.DB) error {
		strategy, _, err := lockAuthority(ctx, tx, authority, execution, current.Input.AccountID, current.Input.InstrumentID)
		if err != nil {
			return err
		}
		instrument, err := lockIntentInstrument(ctx, tx, current.Input.InstrumentID)
		if err != nil {
			return err
		}
		rows, err := lockIntentState(ctx, tx, current)
		if err != nil {
			return err
		}
		if rows.intent.State != string(domain.OrderStateIntent) {
			return domain.ErrOutcomeUnknown
		}
		if rows.intent.ConfigVersion != authority.ConfigVersion {
			return domain.ErrLeaseLost
		}
		if !rows.account.Enabled || !rows.strategy.Enabled || rows.account.Asset != instrument.QuoteAsset {
			return domain.ErrRiskDenied
		}
		risk, now, err := validateRiskSnapshot(ctx, tx, strategy)
		if err != nil {
			return err
		}
		until := min(deadline.UnixMilli(), risk.ExpiresAt)
		if until <= now {
			return domain.ErrRiskDenied
		}
		row := rows.intent
		row.State = string(domain.OrderStateSubmitting)
		row.OwnerId, row.OwnerEpoch = authority.OwnerID, authority.OwnerEpoch
		row.ExecutionOwnerId, row.ExecutionEpoch = execution.OwnerID, execution.Epoch
		row.SubmissionDeadlineAt = until
		row.ErrorCode = ""
		if err := saveIntentState(ctx, tx, rows, now); err != nil {
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

func (s *intentStore) Unknown(ctx context.Context, current domain.IntentSnapshot, code string) (domain.IntentSnapshot, error) {
	if code != domain.IntentErrorOutcomeUnknown && code != domain.IntentErrorCoreNotFound {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return s.changeIntent(ctx, current, func(row *modelv1.OrderIntent) (bool, error) {
		if terminalIntent(row.State) || row.State == string(domain.OrderStateIntent) {
			return false, nil
		}
		if row.State == string(domain.OrderStateUnknown) && row.ErrorCode == code {
			return false, nil
		}
		row.State = string(domain.OrderStateUnknown)
		row.ErrorCode = code
		return true, nil
	})
}

func (s *intentStore) Reject(ctx context.Context, current domain.IntentSnapshot, code string) (domain.IntentSnapshot, error) {
	if code != domain.IntentErrorCoreRejected && code != domain.IntentErrorRiskDenied {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return s.changeIntent(ctx, current, func(row *modelv1.OrderIntent) (bool, error) {
		if (row.State != string(domain.OrderStateSubmitting) && row.State != string(domain.OrderStateIntent)) || row.CoreOrderId != nil {
			return false, domain.ErrIdempotencyConflict
		}
		row.State = string(domain.OrderStateRejected)
		row.ErrorCode = code
		row.ReservedQuoteNotional = "0"
		return true, nil
	})
}

func (s *intentStore) changeIntent(ctx context.Context, current domain.IntentSnapshot, change func(*modelv1.OrderIntent) (bool, error)) (domain.IntentSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.IntentSnapshot{}, err
	}
	var result domain.IntentSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(func(tx *gorm.DB) error {
		rows, err := lockIntentState(ctx, tx, current)
		if err != nil {
			return err
		}
		before, err := decimal.NewFromString(rows.intent.ReservedQuoteNotional)
		if err != nil {
			return err
		}
		changed, err := change(rows.intent)
		if err != nil {
			return err
		}
		if changed {
			if err := releaseIntentReservation(ctx, tx, rows, before); err != nil {
				return err
			}
			now, err := intentClock(ctx, tx, rows.intent.Id)
			if err != nil {
				return err
			}
			if err := saveIntentState(ctx, tx, rows, now); err != nil {
				return err
			}
		}
		result, err = intentSnapshot(rows.intent)
		return err
	})
	if err != nil {
		return domain.IntentSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func lockIntentState(ctx context.Context, tx *gorm.DB, current domain.IntentSnapshot) (intentStateRows, error) {
	if current.ID <= 0 || current.Revision <= 0 || !coreIdentifier(current.Input.AccountID, 64) || current.Input.Authority.StrategyID <= 0 {
		return intentStateRows{}, domain.ErrInvalidOrder
	}
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", current.Input.AccountID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&account).Error; err != nil {
		return intentStateRows{}, err
	}
	var strategy modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", current.Input.Authority.StrategyID, current.Input.AccountID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&strategy).Error; err != nil {
		return intentStateRows{}, err
	}
	var row modelv1.OrderIntent
	// Gateway commands hold an FK KEY SHARE lock before their final budget
	// check. State updates do not change keys; NO KEY UPDATE lets recovery
	// finish without a budget->intent / intent->budget lock cycle.
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND delete_at IS NULL", current.Input.AccountID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&row, current.ID).Error; err != nil {
		return intentStateRows{}, err
	}
	if row.Revision != current.Revision || row.StrategyInstanceId != strategy.StrategyInstanceId {
		return intentStateRows{}, domain.ErrIdempotencyConflict
	}
	return intentStateRows{
		intent:   &row,
		account:  &account,
		strategy: &strategy,
	}, nil
}

func releaseIntentReservation(ctx context.Context, tx *gorm.DB, rows intentStateRows, before decimal.Decimal) error {
	after, err := decimal.NewFromString(rows.intent.ReservedQuoteNotional)
	if err != nil {
		return err
	}
	delta := after.Sub(before)
	if delta.IsPositive() {
		return domain.ErrInvalidOrder
	}
	if delta.IsZero() {
		return nil
	}
	account, err := reserveWithinLimit(rows.account.ReservedNotional, rows.account.LimitNotional, delta)
	if err != nil {
		return err
	}
	strategy, err := reserveWithinLimit(rows.strategy.ReservedNotional, rows.strategy.LimitNotional, delta)
	if err != nil {
		return err
	}
	if err := saveAccountReservation(ctx, tx, rows.account, account); err != nil {
		return err
	}
	return saveStrategyReservation(ctx, tx, rows.strategy, strategy)
}

func intentClock(ctx context.Context, tx *gorm.DB, id int64) (int64, error) {
	var clock struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
	return clock.NowMS, err
}

func saveIntentState(ctx context.Context, tx *gorm.DB, rows intentStateRows, now int64) error {
	row := rows.intent
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("State", "CancelState", "CoreOrderId", "CoreRevision", "FilledQuantity", "ReservedQuoteNotional", "OwnerId", "OwnerEpoch", "ExecutionOwnerId", "ExecutionEpoch", "SubmissionDeadlineAt", "ErrorCode", "Revision").Updates(row)
	if err := reservationResult(result); err != nil {
		return err
	}
	return appendIntentEvent(ctx, tx, row, now, domain.EventOrderStateChanged)
}

func terminalIntent(state string) bool {
	return state == string(domain.OrderStateFilled) || state == string(domain.OrderStateCanceled) || state == string(domain.OrderStateRejected)
}
