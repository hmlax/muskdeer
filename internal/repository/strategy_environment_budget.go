package repository

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func strategyPlanningBudget(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, quoteAsset string) (decimal.Decimal, decimal.Decimal, error) {
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", strategy.AccountId).First(&account).Error; err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	var own modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", strategy.Id, strategy.AccountId).First(&own).Error; err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	if !account.Enabled || !own.Enabled || account.Asset != quoteAsset {
		return decimal.Zero, decimal.Zero, domain.ErrRiskDenied
	}
	reserved, err := domain.ParseLedgerDecimal(own.ReservedNotional, false, true)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	available, err := planningCapacity(own.LimitNotional, own.ReservedNotional, reserved)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	accountCapacity, err := planningCapacity(account.LimitNotional, account.ReservedNotional, reserved)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	available = decimal.Min(available, accountCapacity)
	var global []*modelv1.RiskBudget
	if err := tx.WithContext(ctx).Model(&modelv1.RiskBudget{}).
		Where("asset = ? AND delete_at IS NULL AND ((scope_kind = ? AND scope_key = ?) OR (scope_kind = ? AND scope_key = ?))",
			quoteAsset, domain.RiskScopePlatform, quoteAsset, domain.RiskScopeInstrument, strconv.FormatInt(strategy.InstrumentId, 10)).Order("id ASC").Find(&global).Error; err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	if len(global) != 2 {
		return decimal.Zero, decimal.Zero, domain.ErrRiskDenied
	}
	maximum := decimal.Zero
	for index, row := range global {
		if !row.Enabled {
			return decimal.Zero, decimal.Zero, domain.ErrRiskDenied
		}
		capacity, err := planningCapacity(row.LimitNotional, row.ReservedNotional, reserved)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		available = decimal.Min(available, capacity)
		limit, err := domain.ParseLedgerDecimal(row.MaximumOrderNotional, false, false)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		if index == 0 || limit.LessThan(maximum) {
			maximum = limit
		}
	}
	return available, maximum, nil
}

func planningCapacity(limitText, reservedText string, own decimal.Decimal) (decimal.Decimal, error) {
	limit, err := domain.ParseLedgerDecimal(limitText, false, false)
	if err != nil {
		return decimal.Zero, err
	}
	reserved, err := domain.ParseLedgerDecimal(reservedText, false, true)
	if err != nil {
		return decimal.Zero, err
	}
	if reserved.LessThan(own) || reserved.GreaterThan(limit) {
		return decimal.Zero, domain.ErrRiskDenied
	}
	return limit.Sub(reserved).Add(own), nil
}
