package repository

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func strategyPlanningFunds(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, result *domain.StrategyEnvironmentView) error {
	var balances []*modelv1.SimulatorBalance
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).
		Where("account_id = ? AND asset IN ? AND delete_at IS NULL", strategy.AccountId, []string{result.BaseAsset, result.QuoteAsset}).Find(&balances).Error; err != nil {
		return err
	}
	if len(balances) != 2 || result.BaseAsset == result.QuoteAsset {
		return domain.ErrRiskDenied
	}
	frozenBase, frozenQuote := decimal.Zero, decimal.Zero
	for _, row := range balances {
		available, err := domain.ParseLedgerDecimal(row.Available, false, true)
		if err != nil {
			return err
		}
		frozen, err := domain.ParseLedgerDecimal(row.Frozen, false, true)
		if err != nil {
			return err
		}
		if row.Asset == result.BaseAsset {
			result.Inventory, result.AvailableBase, frozenBase = available.Add(frozen), available, frozen
		} else {
			result.AvailableQuote, frozenQuote = available, frozen
		}
	}
	var orders []*modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("FrozenAsset", "FrozenAmount").
		Where("account_id = ? AND strategy_instance_id = ? AND instrument_id = ? AND state IN ? AND delete_at IS NULL",
			strategy.AccountId, strategy.Id, strategy.InstrumentId, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}).
		Order("id ASC").Limit(domain.MaxStrategyOpenOrders + 1).Find(&orders).Error; err != nil {
		return err
	}
	if len(orders) > domain.MaxStrategyOpenOrders {
		return domain.ErrRiskDenied
	}
	base, quote := decimal.Zero, decimal.Zero
	for _, row := range orders {
		amount, err := domain.ParseLedgerDecimal(row.FrozenAmount, false, true)
		if err != nil {
			return err
		}
		switch row.FrozenAsset {
		case result.BaseAsset:
			base = base.Add(amount)
		case result.QuoteAsset:
			quote = quote.Add(amount)
		default:
			return domain.ErrRiskDenied
		}
	}
	if base.GreaterThan(frozenBase) || quote.GreaterThan(frozenQuote) {
		return domain.ErrRiskDenied
	}
	result.AvailableBase = result.AvailableBase.Add(base)
	result.AvailableQuote = result.AvailableQuote.Add(quote)
	return strategyPlanningBuyRoom(ctx, tx, strategy, result)
}

func strategyPlanningBuyRoom(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, result *domain.StrategyEnvironmentView) error {
	limit, err := strictestInventoryLimit(ctx, tx, strategy.AccountId, strategy.InstrumentId)
	if err != nil {
		return err
	}
	var other struct{ Quantity string }
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("account_id = ? AND instrument_id = ? AND strategy_instance_id <> ? AND side = ? AND state IN ? AND delete_at IS NULL",
			strategy.AccountId, strategy.InstrumentId, strategy.Id, domain.SideBuy,
			[]domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown}).
		Select("COALESCE(sum(quantity - filled_quantity), 0)::text AS quantity").Take(&other).Error; err != nil {
		return err
	}
	buys, err := domain.ParseLedgerDecimal(other.Quantity, false, true)
	if err != nil {
		return err
	}
	result.MaximumBuyQuantity = decimal.Max(decimal.Zero, limit.Sub(result.Inventory).Sub(buys))
	return nil
}
