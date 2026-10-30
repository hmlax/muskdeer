package repository

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type strategyOrderCancellations struct{ db *gorm.DB }

// NewStrategyOrderCancellations shares the existing action journal and fencing.
func NewStrategyOrderCancellations(db *gorm.DB) (domain.StrategyOrderCancellations, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyOrderCancellations{db: db}, nil
}

func (s *strategyOrderCancellations) Prepare(ctx context.Context, authority domain.StrategyAuthority, afterID int64) (domain.AdminCancellationWork, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminCancellationWork{}, err
	}
	if afterID < 0 {
		return domain.AdminCancellationWork{}, domain.ErrInvalidOrder
	}
	var result domain.AdminCancellationWork
	err := s.db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Transaction(func(tx *gorm.DB) error {
		instance, now, err := lockActionAuthority(ctx, tx, authority)
		if err != nil {
			return err
		}
		var receipt modelv1.AdminOrderCancellation
		err = tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
			Where("strategy_instance_id = ? AND state = ? AND id > ?", authority.StrategyID, domain.AdminCancellationAccepted, afterID).
			Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Take(&receipt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result.ReceiptID = receipt.Id
		result.Action, err = prepareAdminCancellation(ctx, tx, instance, authority, &receipt, now)
		return err
	})
	if err != nil {
		return domain.AdminCancellationWork{}, actionFailure(ctx, err)
	}
	return result, nil
}

func prepareAdminCancellation(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, authority domain.StrategyAuthority, receipt *modelv1.AdminOrderCancellation, now int64) (*domain.StrategyAction, error) {
	var order modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("id = ? AND strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL", receipt.OrderIntentId, instance.Id, instance.AccountId, instance.InstrumentId).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&order).Error; err != nil {
		return nil, err
	}
	if terminalIntent(order.State) {
		reserved, err := domain.ParseLedgerDecimal(order.ReservedQuoteNotional, false, true)
		if err != nil {
			return nil, err
		}
		if !reserved.IsZero() {
			return nil, domain.ErrOutcomeUnknown
		}
		return nil, finishAdminCancellation(ctx, tx, receipt, &order, now, domain.AdminCancellationSucceeded, domain.AdminCancellationTerminal)
	}
	action, err := adminCancellationAction(ctx, tx, instance, authority, receipt, &order)
	if err != nil || action == nil {
		return nil, err
	}
	switch domain.StrategyActionState(action.State) {
	case domain.StrategyActionNotAccepted:
		return nil, finishAdminCancellation(ctx, tx, receipt, &order, now, domain.AdminCancellationFailed, domain.AdminCancellationNotAccepted)
	case domain.StrategyActionObserved:
		if action.ObservedCancellationState == string(domain.CancellationRejected) {
			return nil, finishAdminCancellation(ctx, tx, receipt, &order, now, domain.AdminCancellationFailed, domain.AdminCancellationRejected)
		}
	case domain.StrategyActionPending:
		snapshot, err := actionSnapshot(action)
		return &snapshot, err
	}
	return nil, nil
}

func adminCancellationAction(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, authority domain.StrategyAuthority, receipt *modelv1.AdminOrderCancellation, order *modelv1.OrderIntent) (*modelv1.StrategyAction, error) {
	var action modelv1.StrategyAction
	query := tx.WithContext(ctx).Model(&modelv1.StrategyAction{})
	if receipt.ActionId != nil {
		if err := query.Where("id = ?", *receipt.ActionId).Take(&action).Error; err != nil {
			return nil, err
		}
		return &action, nil
	}
	// Adopt a cancellation already being performed by protection or another
	// manager. Never create a competing command for the same unresolved target.
	err := query.Where("strategy_instance_id = ? AND kind = ? AND intent_id = ? AND state IN ? AND delete_at IS NULL",
		instance.Id, domain.StrategyActionCancel, order.Id, activeActionStates()).Take(&action).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		plan := domain.StrategyActionPlan{
			Key:                 domain.AdminCancellationDecisionPrefix + strconv.FormatInt(receipt.Id, 10),
			Kind:                domain.StrategyActionCancel,
			AccountID:           order.AccountId,
			InstrumentID:        order.InstrumentId,
			TargetIntentID:      order.Id,
			TargetClientOrderID: order.ClientOrderId,
		}
		if err := actionPlanCapacity(ctx, tx, instance, plan); err != nil {
			if errors.Is(err, domain.ErrOutcomeUnknown) {
				return nil, nil
			}
			return nil, err
		}
		created, err := newActionRow(authority, plan)
		if err != nil {
			return nil, err
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Create(created).Error; err != nil {
			return nil, err
		}
		return linkAdminCancellation(ctx, tx, receipt, created)
	}
	return linkAdminCancellation(ctx, tx, receipt, &action)
}

func linkAdminCancellation(ctx context.Context, tx *gorm.DB, receipt *modelv1.AdminOrderCancellation, action *modelv1.StrategyAction) (*modelv1.StrategyAction, error) {
	receipt.ActionId = proto.Int64(action.Id)
	if err := saveAdminCancellation(ctx, tx, receipt); err != nil {
		return nil, err
	}
	return action, nil
}

func finishAdminCancellation(ctx context.Context, tx *gorm.DB, receipt *modelv1.AdminOrderCancellation, order *modelv1.OrderIntent, now int64, state domain.AdminCancellationState, outcome string) error {
	receipt.State, receipt.OutcomeCode = string(state), outcome
	receipt.CompletedAt = proto.Int64(now)
	receipt.ObservedOrderRevision = proto.Int64(order.Revision)
	receipt.ObservedOrderState = proto.String(order.State)
	return saveAdminCancellation(ctx, tx, receipt)
}

func saveAdminCancellation(ctx context.Context, tx *gorm.DB, row *modelv1.AdminOrderCancellation) error {
	previous := row.Revision
	row.Revision++
	return reservationResult(tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
		Where("id = ? AND revision = ?", row.Id, previous).
		Select("ActionId", "State", "OutcomeCode", "Revision", "CompletedAt", "ObservedOrderRevision", "ObservedOrderState").Updates(row))
}

var _ domain.StrategyOrderCancellations = (*strategyOrderCancellations)(nil)
