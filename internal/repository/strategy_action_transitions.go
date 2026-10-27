package repository

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *strategyActions) Attempt(ctx context.Context, current domain.StrategyAction, authority domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	return s.changeAction(ctx, current, authority, func(_ *gorm.DB, row *modelv1.StrategyAction, strategy *modelv1.StrategyInstance, now int64) (bool, error) {
		if row.State != string(domain.StrategyActionPending) {
			return false, domain.ErrOutcomeUnknown
		}
		native, _ := ctx.Deadline()
		if deadline.UnixMilli() <= now || deadline.After(native) || deadline.UnixMilli()-now > domain.MaxIntentLifetime.Milliseconds() || row.ConfigVersion != authority.ConfigVersion {
			return false, domain.ErrInvalidOrder
		}
		if row.Kind == string(domain.StrategyActionSubmit) && (strategy.State != string(domain.StrategyStateRunning) || !strategy.DesiredRunning) {
			return false, domain.ErrRiskDenied
		}
		row.State, row.DeadlineAt = string(domain.StrategyActionDispatching), deadline.UnixMilli()
		return true, nil
	})
}

func (s *strategyActions) Discard(ctx context.Context, current domain.StrategyAction, authority domain.StrategyAuthority) (domain.StrategyAction, error) {
	return s.changeAction(ctx, current, authority, func(_ *gorm.DB, row *modelv1.StrategyAction, _ *modelv1.StrategyInstance, _ int64) (bool, error) {
		if row.State == string(domain.StrategyActionDiscarded) {
			return false, nil
		}
		if row.State != string(domain.StrategyActionPending) {
			return false, domain.ErrOutcomeUnknown
		}
		row.State = string(domain.StrategyActionDiscarded)
		return true, nil
	})
}

func (s *strategyActions) Observe(ctx context.Context, current domain.StrategyAction, authority domain.StrategyAuthority) (domain.StrategyAction, error) {
	return s.changeAction(ctx, current, authority, func(tx *gorm.DB, row *modelv1.StrategyAction, _ *modelv1.StrategyInstance, _ int64) (bool, error) {
		if row.State == string(domain.StrategyActionObserved) {
			return false, nil
		}
		if row.State != string(domain.StrategyActionDispatching) && row.State != string(domain.StrategyActionWaiting) && row.State != string(domain.StrategyActionUnknown) {
			return false, domain.ErrInvalidOrder
		}
		state, intent, cancellation := row.State, row.GetIntentId(), row.GetCancellationId()
		orderState, cancelState := row.ObservedOrderState, row.ObservedCancellationState
		row.State = string(domain.StrategyActionUnknown)
		var err error
		if row.Kind == string(domain.StrategyActionSubmit) {
			err = observeActionSubmit(ctx, tx, row)
		} else {
			err = observeActionCancel(ctx, tx, row)
		}
		if err != nil {
			return false, err
		}
		changed := row.State != state || row.GetIntentId() != intent || row.GetCancellationId() != cancellation ||
			row.ObservedOrderState != orderState || row.ObservedCancellationState != cancelState || row.OwnerId != authority.OwnerID || row.OwnerEpoch != authority.OwnerEpoch
		return changed, nil
	})
}

func (s *strategyActions) changeAction(ctx context.Context, current domain.StrategyAction, authority domain.StrategyAuthority, change func(*gorm.DB, *modelv1.StrategyAction, *modelv1.StrategyInstance, int64) (bool, error)) (domain.StrategyAction, error) {
	if err := coreContext(ctx); err != nil {
		return domain.StrategyAction{}, err
	}
	if current.ID <= 0 || current.Revision <= 0 || current.StrategyID != authority.StrategyID {
		return domain.StrategyAction{}, domain.ErrInvalidOrder
	}
	if err := actionAuthorityFields(authority); err != nil {
		return domain.StrategyAction{}, err
	}
	var result domain.StrategyAction
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Transaction(func(tx *gorm.DB) error {
		strategy, now, err := lockActionAuthority(ctx, tx, authority)
		if err != nil {
			return err
		}
		var row modelv1.StrategyAction
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).
			Where("id = ? AND strategy_instance_id = ? AND delete_at IS NULL", current.ID, authority.StrategyID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if row.Revision != current.Revision {
			return domain.ErrIdempotencyConflict
		}
		changed, err := change(tx, &row, strategy, now)
		if err != nil {
			return err
		}
		if changed {
			if err := saveActionState(ctx, tx, &row, authority); err != nil {
				return err
			}
		}
		result, err = actionSnapshot(&row)
		return err
	})
	if err != nil {
		return domain.StrategyAction{}, actionFailure(ctx, err)
	}
	return result, nil
}

func saveActionState(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyAction, authority domain.StrategyAuthority) error {
	previous := row.Revision
	row.Revision++
	row.OwnerId, row.OwnerEpoch = authority.OwnerID, authority.OwnerEpoch
	result := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("State", "DeadlineAt", "IntentId", "CancellationId", "ObservedOrderState", "ObservedCancellationState", "OwnerId", "OwnerEpoch", "Revision").Updates(row)
	return reservationResult(result)
}

func observeActionSubmit(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyAction) error {
	var intent modelv1.OrderIntent
	err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("account_id = ? AND idempotency_key = ? AND delete_at IS NULL", row.AccountId, row.IdempotencyKey).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&intent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := intentSnapshot(&intent)
	if err != nil {
		return err
	}
	planned, err := actionSnapshot(row)
	if err != nil {
		return err
	}
	p := planned.Plan
	if actual.ClientOrderID != row.ClientOrderId || actual.Input.Authority.StrategyID != row.StrategyInstanceId || actual.Input.Authority.ConfigVersion != row.ConfigVersion ||
		actual.Input.InstrumentID != row.InstrumentId || actual.Input.Side != p.Side || actual.Input.TimeInForce != p.TimeInForce || actual.Input.ReduceOnly != p.ReduceOnly ||
		!actual.Input.Price.Equal(p.Price) || !actual.Input.Quantity.Equal(p.Quantity) {
		return domain.ErrInvalidOrder
	}
	row.IntentId, row.ObservedOrderState = proto.Int64(intent.Id), intent.State
	switch actual.State {
	case domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateFilled, domain.OrderStateCanceled, domain.OrderStateRejected:
		row.State = string(domain.StrategyActionObserved)
	case domain.OrderStateIntent, domain.OrderStateSubmitting:
		row.State = string(domain.StrategyActionWaiting)
	case domain.OrderStateUnknown:
		row.State = string(domain.StrategyActionUnknown)
	default:
		return domain.ErrInvalidOrder
	}
	return nil
}

func observeActionCancel(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyAction) error {
	var request modelv1.CancellationRequest
	err := tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).
		Where("account_id = ? AND idempotency_key = ? AND delete_at IS NULL", row.AccountId, row.IdempotencyKey).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if request.IntentId != row.GetIntentId() || request.ClientOrderId != row.ClientOrderId || request.StrategyInstanceId != row.StrategyInstanceId || request.InstrumentId != row.InstrumentId {
		return domain.ErrInvalidOrder
	}
	var parent modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ? AND delete_at IS NULL", request.IntentId).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&parent).Error; err != nil {
		return err
	}
	row.CancellationId, row.ObservedCancellationState, row.ObservedOrderState = proto.Int64(request.Id), request.State, parent.State
	switch domain.CancellationState(request.State) {
	case domain.CancellationConfirmed:
		if !terminalIntent(parent.State) {
			return domain.ErrOutcomeUnknown
		}
		row.State = string(domain.StrategyActionObserved)
	case domain.CancellationRejected:
		row.State = string(domain.StrategyActionObserved)
	case domain.CancellationRequested, domain.CancellationSubmitting, domain.CancellationPending:
		row.State = string(domain.StrategyActionWaiting)
	case domain.CancellationUnknown:
		row.State = string(domain.StrategyActionUnknown)
	default:
		return domain.ErrInvalidOrder
	}
	return nil
}
