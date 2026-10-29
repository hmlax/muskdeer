package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *adminStrategyRisk) State(ctx context.Context, token string, strategyID int64) (domain.AdminRiskState, error) {
	if strategyID <= 0 {
		return domain.AdminRiskState{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminRiskState
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, strategyID)
		if err != nil {
			return err
		}
		var budget modelv1.StrategyBudget
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).
			Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", instance.Id, instance.AccountId).First(&budget).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		var account modelv1.AccountBudget
		if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", instance.AccountId).First(&account).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		limit, err := domain.ParseLedgerDecimal(budget.LimitNotional, false, false)
		if err != nil {
			return err
		}
		reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil {
			return err
		}
		revision, err := adminRiskRevision(ctx, tx, instance.Id)
		if err != nil {
			return err
		}
		result = domain.AdminRiskState{
			StrategyID:             instance.Id,
			Enabled:                budget.Enabled,
			Revision:               revision,
			QuoteAsset:             account.Asset,
			LimitNotional:          limit,
			ReservedNotional:       reserved,
			State:                  domain.StrategyState(instance.State),
			DesiredState:           domain.StrategyState(instance.DesiredState),
			RequestedConfigVersion: instance.RequestedConfigVersion,
			Source:                 domain.MarketSource(instance.MarketDataSource),
			UpdatedAt:              time.UnixMilli(budget.UpdateAt).UTC(),
		}
		return nil
	})
	if err != nil {
		return domain.AdminRiskState{}, err
	}
	return result, nil
}
