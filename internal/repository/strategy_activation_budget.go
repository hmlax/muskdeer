package repository

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// prepareActivationBudget runs after the strategy/lease and obligation checks.
// Keep the admission order: instrument -> core account -> account budget -> own
// budget. Locking the shared budget first can deadlock another strategy's core
// writer. All changes roll back with the active pair and final lease fence.
func prepareActivationBudget(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyInstance, replacing bool) error {
	if _, err := lockCoreRows(ctx, tx, row.AccountId, row.InstrumentId); err != nil {
		return err
	}
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", row.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&account).Error; err != nil {
		return err
	}
	var own modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", row.Id, row.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&own).Error; err != nil {
		return err
	}
	reserved, err := domain.ParseLedgerDecimal(own.ReservedNotional, false, true)
	if err != nil {
		return err
	}
	if !reserved.IsZero() {
		return domain.ErrOutcomeUnknown
	}
	if !replacing {
		return nil
	}
	parameters, err := readStrategyParameters(ctx, tx, row)
	if err != nil {
		return err
	}
	limit := strategyParameterBudget(parameters)
	previousLimit, err := domain.ParseLedgerDecimal(own.LimitNotional, false, false)
	if err != nil {
		return err
	}
	if previousLimit.Equal(limit) {
		return nil
	}
	previous := own.Revision
	own.LimitNotional, own.Revision = limit.String(), previous+1
	return reservationResult(tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("id = ? AND revision = ?", own.Id, previous).
		Select("LimitNotional", "Revision").Updates(&own))
}

// strategyParameterBudget accepts validated, concrete template parameters.
func strategyParameterBudget(parameters domain.StrategyParameters) decimal.Decimal {
	if parameters.Grid != nil {
		return parameters.Grid.TotalBudget
	}
	return parameters.Maker.SideBudget.Mul(decimal.NewFromInt(2))
}
