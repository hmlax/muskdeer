package repository

import (
	"context"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// The caller holds the same strategy admission mutex as execution. Once the
// original database deadline has passed, an absent request cannot be accepted
// later. The migration independently proves absence and seals every old key.
func resolveUnacceptedAction(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyAction, now int64) error {
	if row.DeadlineAt <= 0 || row.DeadlineAt > now || row.ObservedOrderState != "" || row.ObservedCancellationState != "" {
		return nil
	}
	var queries []*gorm.DB
	if row.Kind == string(domain.StrategyActionSubmit) {
		if row.IntentId != nil {
			return nil
		}
		queries = []*gorm.DB{
			tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND (idempotency_key = ? OR client_order_id = ?)", row.AccountId, row.IdempotencyKey, row.ClientOrderId),
			tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id = ?", row.AccountId, row.ClientOrderId),
			tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("account_id = ? AND action = ? AND client_order_id = ?", row.AccountId, domain.GatewayActionSubmit, row.ClientOrderId),
		}
	} else {
		if row.CancellationId != nil {
			return nil
		}
		digest, err := domain.IntentClientOrderID(row.AccountId, row.IdempotencyKey)
		if err != nil {
			return err
		}
		queries = []*gorm.DB{
			tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("account_id = ? AND idempotency_key = ?", row.AccountId, row.IdempotencyKey),
			tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("account_id = ? AND action = ? AND command_key = ?", row.AccountId, domain.GatewayActionCancel, domain.CoreCancelKeyPrefix+digest),
		}
	}
	for _, query := range queries {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
	row.State = string(domain.StrategyActionNotAccepted)
	return nil
}
