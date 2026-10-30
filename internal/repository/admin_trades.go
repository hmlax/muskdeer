package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminTrades struct{ snapshots adminStrategies }

// NewAdminTrades borrows storage without starting consumers or changing facts.
func NewAdminTrades(db *gorm.DB) (domain.AdminTrades, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminTrades{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminTrades) List(ctx context.Context, token string, filter domain.AdminTradeFilter) (domain.AdminTradePage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminTradePage{}, err
	}
	var result domain.AdminTradePage
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, filter.StrategyID)
		if err != nil {
			return err
		}
		orders, err := adminTradeOrderQuery(ctx, tx, instance, filter)
		if err != nil {
			return err
		}
		var rows []*modelv1.SimulatorFill
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).
			Where("core_order_id IN (?) AND id > ? AND delete_at IS NULL", orders, filter.Page.AfterID).
			Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Page.Limit {
			rows = rows[:filter.Page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		result.Items, err = readAdminTrades(ctx, tx, instance, rows)
		return err
	})
	if err != nil {
		return domain.AdminTradePage{}, err
	}
	return result, nil
}

func (s *adminTrades) Detail(ctx context.Context, token string, strategyID, tradeID int64) (domain.AdminTrade, error) {
	if strategyID <= 0 || tradeID <= 0 {
		return domain.AdminTrade{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminTrade
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, strategyID)
		if err != nil {
			return err
		}
		orders, err := adminTradeOrderQuery(ctx, tx, instance, domain.AdminTradeFilter{})
		if err != nil {
			return err
		}
		var row modelv1.SimulatorFill
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).
			Where("id = ? AND core_order_id IN (?) AND delete_at IS NULL", tradeID, orders).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		values, err := readAdminTrades(ctx, tx, instance, []*modelv1.SimulatorFill{&row})
		if err != nil {
			return err
		}
		result = values[0]
		return nil
	})
	if err != nil {
		return domain.AdminTrade{}, err
	}
	return result, nil
}

func adminTradeOrderQuery(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, filter domain.AdminTradeFilter) (*gorm.DB, error) {
	query := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("id").
		Where("strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL", instance.Id, instance.AccountId, instance.InstrumentId)
	if filter.Side != domain.SideUnspecified {
		query = query.Where("side = ?", filter.Side)
	}
	if filter.OrderID == 0 {
		return query, nil
	}
	var intent modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("id = ? AND strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL", filter.OrderID, instance.Id, instance.AccountId, instance.InstrumentId).Take(&intent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAdminNotFound
		}
		return nil, err
	}
	commands := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Select("id").
		Where("intent_id = ? AND strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND action = ? AND delete_at IS NULL", intent.Id, instance.Id, instance.AccountId, instance.InstrumentId, domain.GatewayActionSubmit)
	return query.Where("initial_command_id IN (?)", commands), nil
}

var _ domain.AdminTrades = (*adminTrades)(nil)
