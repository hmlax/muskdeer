package repository

import (
	"context"

	"gorm.io/gorm"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func emergencyProgress(ctx context.Context, tx *gorm.DB, acceptanceID, strategyID int64, enrolled bool) (domain.AdminEmergencyCancelProgress, error) {
	result := domain.AdminEmergencyCancelProgress{
		EnrollmentComplete: enrolled,
	}
	children := func() *gorm.DB {
		return tx.WithContext(ctx).Model(&executionmodelv1.AdminOrderCancellation{}).Where("emergency_cancellation_id = ? AND delete_at IS NULL", acceptanceID)
	}
	if err := children().Count(&result.Targets).Error; err != nil {
		return result, err
	}
	activeCoreClients := tx.WithContext(ctx).Model(&executionmodelv1.SimulatorOrder{}).Select("client_order_id").
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategyID, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled})
	terminalOrders := tx.WithContext(ctx).Model(&executionmodelv1.OrderIntent{}).Select("id").
		Where("strategy_instance_id = ? AND state IN ? AND reserved_quote_notional = 0 AND delete_at IS NULL", strategyID, []domain.OrderState{domain.OrderStateFilled, domain.OrderStateCanceled, domain.OrderStateRejected}).
		Where("client_order_id NOT IN (?)", activeCoreClients)
	if err := children().Where("state <> ? AND order_intent_id IN (?)", domain.AdminCancellationAccepted, terminalOrders).Count(&result.Terminal).Error; err != nil {
		return result, err
	}
	knownLiveOrders := tx.WithContext(ctx).Model(&executionmodelv1.OrderIntent{}).Select("id").
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategyID, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled})
	if err := children().Where("state = ? AND order_intent_id IN (?)", domain.AdminCancellationFailed, knownLiveOrders).Count(&result.Failed).Error; err != nil {
		return result, err
	}
	if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyAction{}).
		Where("strategy_instance_id = ? AND kind = ? AND state IN ? AND delete_at IS NULL", strategyID, domain.StrategyActionSubmit, activeActionStates()).Count(&result.PendingSubmissions).Error; err != nil {
		return result, err
	}
	return result, nil
}

func emergencyCoreProjectionClear(ctx context.Context, tx *gorm.DB, strategyID, acceptanceID int64, state domain.AdminEmergencyCancelState) (bool, error) {
	enrolledOrderIDs := tx.WithContext(ctx).Model(&executionmodelv1.AdminOrderCancellation{}).Select("order_intent_id").Where("emergency_cancellation_id = ?", acceptanceID)
	var missing int64
	if err := tx.WithContext(ctx).Model(&executionmodelv1.OrderIntent{}).Where("strategy_instance_id = ? AND delete_at IS NULL", strategyID).
		Where("state IN ? OR reserved_quote_notional <> 0", emergencyActiveOrderStates()).Where("id NOT IN (?)", enrolledOrderIDs).Count(&missing).Error; err != nil {
		return false, err
	}
	if missing != 0 {
		return false, nil
	}
	core := tx.WithContext(ctx).Model(&executionmodelv1.SimulatorOrder{}).
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategyID, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled})
	if state == domain.AdminEmergencyCancelFailed {
		knownClients := tx.WithContext(ctx).Model(&executionmodelv1.OrderIntent{}).Select("client_order_id").Where("id IN (?)", enrolledOrderIDs)
		core = core.Where("client_order_id NOT IN (?)", knownClients)
	}
	if err := core.Count(&missing).Error; err != nil {
		return false, err
	}
	return missing == 0, nil
}

func emergencyActiveOrderStates() []domain.OrderState {
	return []domain.OrderState{
		domain.OrderStateIntent,
		domain.OrderStateSubmitting,
		domain.OrderStateLive,
		domain.OrderStatePartiallyFilled,
		domain.OrderStateUnknown,
	}
}
