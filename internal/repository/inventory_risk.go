package repository

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

// The caller holds the account budget mutex and instrument lock. Configuration
// activation takes that same budget mutex; typed parameter versions are immutable.
func validateWorstInventory(ctx context.Context, tx *gorm.DB, accountID string, instrument *modelv1.Instrument, additional decimal.Decimal) error {
	if !domain.FixedDecimal(additional, 20, 18, true) {
		return domain.ErrRiskDenied
	}
	limit, err := strictestInventoryLimit(ctx, tx, accountID, instrument.Id)
	if err != nil {
		return err
	}
	var balance modelv1.SimulatorBalance
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ? AND delete_at IS NULL", accountID, instrument.BaseAsset).First(&balance).Error; err != nil {
		return err
	}
	inventory, err := coreBalanceTotal(&balance)
	if err != nil {
		return err
	}
	var remaining struct{ Quantity string }
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND instrument_id = ? AND side = ? AND delete_at IS NULL AND state IN ?", accountID, instrument.Id, string(domain.SideBuy), []string{string(domain.OrderStateIntent), string(domain.OrderStateSubmitting), string(domain.OrderStateLive), string(domain.OrderStatePartiallyFilled), string(domain.OrderStateUnknown)}).Select("COALESCE(sum(quantity - filled_quantity), 0)::text AS quantity").Take(&remaining).Error; err != nil {
		return err
	}
	buys, err := domain.ParseLedgerDecimal(remaining.Quantity, false, true)
	if err != nil {
		return err
	}
	if !domain.FixedDecimal(inventory, 40, 36, true) || inventory.Add(buys).Add(additional).GreaterThan(limit) {
		return domain.ErrRiskDenied
	}
	return nil
}

func strictestInventoryLimit(ctx context.Context, tx *gorm.DB, accountID string, instrumentID int64) (decimal.Decimal, error) {
	active := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Select("id, active_config_version").Where("account_id = ? AND instrument_id = ? AND active_config_version IS NOT NULL AND delete_at IS NULL", accountID, instrumentID)
	var versions []*modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("(instance_id, version) IN (?) AND delete_at IS NULL", active).Limit(domain.MaxAccountInventoryPolicies + 1).Find(&versions).Error; err != nil {
		return decimal.Zero, err
	}
	if len(versions) == 0 || len(versions) > domain.MaxAccountInventoryPolicies {
		return decimal.Zero, domain.ErrRiskDenied
	}
	limit := decimal.Zero
	for _, version := range versions {
		value, err := inventoryParameterLimit(version)
		if err != nil {
			return decimal.Zero, err
		}
		if limit.IsZero() || value.LessThan(limit) {
			limit = value
		}
	}
	return limit, nil
}

func inventoryParameterLimit(version *modelv1.StrategyConfigVersion) (decimal.Decimal, error) {
	if version.ParametersSchemaVersion != domain.ParametersSchemaVersion {
		return decimal.Zero, domain.ErrRiskDenied
	}
	switch domain.StrategyTemplate(version.GetTemplate()) {
	case domain.StrategyTemplateMarketMaker:
		if version.GridParameters != nil {
			return decimal.Zero, domain.ErrRiskDenied
		}
		parameters, err := strategyconfig.Maker(version.MakerParameters)
		if err != nil {
			return decimal.Zero, err
		}
		return parameters.InventoryHardLimit, nil
	case domain.StrategyTemplateGrid:
		if version.MakerParameters != nil {
			return decimal.Zero, domain.ErrRiskDenied
		}
		parameters, err := strategyconfig.Grid(version.GridParameters)
		if err != nil {
			return decimal.Zero, err
		}
		return parameters.InventoryLimit, nil
	default:
		return decimal.Zero, domain.ErrRiskDenied
	}
}
