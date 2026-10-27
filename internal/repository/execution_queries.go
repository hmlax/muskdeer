package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type executionQueries struct{ db *gorm.DB }

// NewExecutionQueries borrows the configured database without provisioning.
func NewExecutionQueries(db *gorm.DB) (domain.ExecutionQueries, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &executionQueries{db: db}, nil
}

func (s *executionQueries) Orders(ctx context.Context, filter domain.ExecutionFilter) ([]domain.IntentSnapshot, int64, error) {
	if err := executionFilter(ctx, filter); err != nil {
		return nil, 0, err
	}
	query := s.db.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("account_id = ? AND id > ? AND delete_at IS NULL", filter.AccountID, filter.AfterID)
	if filter.StrategyID > 0 {
		query = query.Where("strategy_instance_id = ?", filter.StrategyID)
	}
	if filter.InstrumentID > 0 {
		query = query.Where("instrument_id = ?", filter.InstrumentID)
	}
	if filter.OpenOnly {
		query = query.Where("state IN ?", []domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown})
	}
	var rows []*modelv1.OrderIntent
	if err := query.Order("id ASC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
		return nil, 0, coreFailure(ctx, err)
	}
	var next int64
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
		next = rows[len(rows)-1].Id
	}
	orders := make([]domain.IntentSnapshot, 0, len(rows))
	for _, row := range rows {
		order, err := intentSnapshot(row)
		if err != nil {
			return nil, 0, coreFailure(ctx, err)
		}
		orders = append(orders, order)
	}
	return orders, next, nil
}

func (s *executionQueries) Fills(ctx context.Context, filter domain.ExecutionFilter) ([]domain.ExecutionFill, int64, error) {
	if err := executionFilter(ctx, filter); err != nil {
		return nil, 0, err
	}
	if filter.OpenOnly {
		return nil, 0, domain.ErrInvalidOrder
	}
	orders := s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("id").Where("account_id = ? AND delete_at IS NULL", filter.AccountID)
	if filter.StrategyID > 0 {
		orders = orders.Where("strategy_instance_id = ?", filter.StrategyID)
	}
	if filter.InstrumentID > 0 {
		orders = orders.Where("instrument_id = ?", filter.InstrumentID)
	}
	var rows []*modelv1.SimulatorFill
	if err := s.db.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Where("core_order_id IN (?) AND id > ? AND delete_at IS NULL", orders, filter.AfterID).
		Order("id ASC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
		return nil, 0, coreFailure(ctx, err)
	}
	var next int64
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
		next = rows[len(rows)-1].Id
	}
	return s.executionFills(ctx, rows, next)
}

func (s *executionQueries) executionFills(ctx context.Context, rows []*modelv1.SimulatorFill, next int64) ([]domain.ExecutionFill, int64, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.CoreOrderId)
	}
	var orders []*modelv1.SimulatorOrder
	if len(ids) != 0 {
		if err := s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("id IN ? AND delete_at IS NULL", ids).Find(&orders).Error; err != nil {
			return nil, 0, coreFailure(ctx, err)
		}
	}
	byID := make(map[int64]*modelv1.SimulatorOrder, len(orders))
	for _, order := range orders {
		byID[order.Id] = order
	}
	fills := make([]domain.ExecutionFill, 0, len(rows))
	for _, row := range rows {
		order, found := byID[row.CoreOrderId]
		if !found {
			return nil, 0, domain.ErrCoreNotFound
		}
		fill, err := coreFill(row)
		if err != nil {
			return nil, 0, coreFailure(ctx, err)
		}
		fills = append(fills, domain.ExecutionFill{
			Fill:          fill,
			AccountID:     order.AccountId,
			ClientOrderID: order.ClientOrderId,
			StrategyID:    order.StrategyInstanceId,
			InstrumentID:  order.InstrumentId,
			Side:          domain.Side(order.Side),
			OccurredAt:    time.UnixMilli(row.CreateAt).UTC(),
		})
	}
	return fills, next, nil
}

func (s *executionQueries) Account(ctx context.Context, accountID string) (domain.ExecutionAccount, error) {
	if err := coreContext(ctx); err != nil {
		return domain.ExecutionAccount{}, err
	}
	if !coreIdentifier(accountID, 64) {
		return domain.ExecutionAccount{}, domain.ErrInvalidOrder
	}
	var result domain.ExecutionAccount
	err := s.db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Transaction(func(tx *gorm.DB) error {
		var account modelv1.SimulatorAccount
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ? AND delete_at IS NULL", accountID).
			Clauses(clause.Locking{Strength: "SHARE"}).First(&account).Error; err != nil {
			return err
		}
		var rows []*modelv1.SimulatorBalance
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND delete_at IS NULL", accountID).
			Order("asset ASC").Limit(domain.MaximumExecutionAssets + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > domain.MaximumExecutionAssets {
			return domain.ErrInvalidOrder
		}
		now, err := businessClock(ctx, tx)
		if err != nil {
			return err
		}
		result = domain.ExecutionAccount{
			AccountID:  accountID,
			Sequence:   account.Revision,
			ObservedAt: time.UnixMilli(now).UTC(),
			Balances:   make([]domain.CoreBalance, 0, len(rows)),
		}
		for _, row := range rows {
			available, err := decimal.NewFromString(row.Available)
			if err != nil {
				return err
			}
			frozen, err := decimal.NewFromString(row.Frozen)
			if err != nil {
				return err
			}
			result.Balances = append(result.Balances, domain.CoreBalance{
				Asset:     row.Asset,
				Available: available,
				Frozen:    frozen,
			})
		}
		return nil
	})
	if err != nil {
		return domain.ExecutionAccount{}, coreFailure(ctx, err)
	}
	return result, nil
}

func executionFilter(ctx context.Context, filter domain.ExecutionFilter) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	if !coreIdentifier(filter.AccountID, 64) || filter.StrategyID < 0 || filter.InstrumentID < 0 || filter.AfterID < 0 || filter.Limit < 1 || filter.Limit > domain.MaximumExecutionPage {
		return domain.ErrInvalidOrder
	}
	return nil
}
