package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminOrders struct{ snapshots adminStrategies }

// NewAdminOrders borrows reads without creating resources or changing orders.
func NewAdminOrders(db *gorm.DB) (domain.AdminOrders, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminOrders{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminOrders) List(ctx context.Context, token string, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminOrderPage{}, err
	}
	var result domain.AdminOrderPage
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, filter.StrategyID)
		if err != nil {
			return err
		}
		query := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
			Where("strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND id > ? AND delete_at IS NULL", instance.Id, instance.AccountId, instance.InstrumentId, filter.Page.AfterID)
		if filter.State != domain.OrderStateUnspecified {
			query = query.Where("state = ?", filter.State)
		}
		if filter.OpenOnly {
			query = query.Where("state IN ?", []domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown})
		}
		var rows []*modelv1.OrderIntent
		if err := query.Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Page.Limit {
			rows = rows[:filter.Page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		result.Items, err = readAdminOrders(ctx, tx, instance, rows)
		return err
	})
	if err != nil {
		return domain.AdminOrderPage{}, err
	}
	return result, nil
}

func (s *adminOrders) Detail(ctx context.Context, token string, strategyID, orderID int64) (domain.AdminOrder, error) {
	if strategyID <= 0 || orderID <= 0 {
		return domain.AdminOrder{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminOrder
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, strategyID)
		if err != nil {
			return err
		}
		var row modelv1.OrderIntent
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
			Where("id = ? AND strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL", orderID, instance.Id, instance.AccountId, instance.InstrumentId).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		values, err := readAdminOrders(ctx, tx, instance, []*modelv1.OrderIntent{&row})
		if err != nil {
			return err
		}
		result = values[0]
		return nil
	})
	if err != nil {
		return domain.AdminOrder{}, err
	}
	return result, nil
}

func readAdminOrders(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, rows []*modelv1.OrderIntent) ([]domain.AdminOrder, error) {
	result := make([]domain.AdminOrder, 0, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	versions := make([]int64, 0, len(rows))
	clients := make([]string, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, row.ConfigVersion)
		clients = append(clients, row.ClientOrderId)
	}
	var configs []*modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
		Select("Id", "Version", "MarketDataSource").Where("instance_id = ? AND version IN ? AND delete_at IS NULL", instance.Id, versions).Find(&configs).Error; err != nil {
		return nil, err
	}
	sources := make(map[int64]domain.MarketSource, len(configs))
	for _, config := range configs {
		source := domain.MarketSource(config.MarketDataSource)
		if err := source.ValidateSelection(); err != nil {
			return nil, err
		}
		sources[config.Version] = source
	}
	var cores []*modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).
		Where("strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND client_order_id IN ? AND delete_at IS NULL", instance.Id, instance.AccountId, instance.InstrumentId, clients).Find(&cores).Error; err != nil {
		return nil, err
	}
	byClient := make(map[string]*modelv1.SimulatorOrder, len(cores))
	for _, core := range cores {
		byClient[core.ClientOrderId] = core
	}
	for _, row := range rows {
		source, found := sources[row.ConfigVersion]
		if !found {
			return nil, domain.ErrAdminUnavailable
		}
		value, err := adminOrderValue(row, source, byClient[row.ClientOrderId])
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func adminOrderValue(row *modelv1.OrderIntent, source domain.MarketSource, core *modelv1.SimulatorOrder) (domain.AdminOrder, error) {
	intent, err := intentSnapshot(row)
	if err != nil {
		return domain.AdminOrder{}, err
	}
	result := domain.AdminOrder{
		ID:                    intent.ID,
		ClientOrderID:         intent.ClientOrderID,
		StrategyID:            intent.Input.Authority.StrategyID,
		AccountID:             intent.Input.AccountID,
		InstrumentID:          intent.Input.InstrumentID,
		ConfigVersion:         intent.Input.Authority.ConfigVersion,
		Source:                source,
		Side:                  intent.Input.Side,
		Price:                 intent.Input.Price,
		Quantity:              intent.Input.Quantity,
		FilledQuantity:        intent.FilledQuantity,
		ReservedNotional:      intent.ReservedQuoteNotional,
		TimeInForce:           intent.Input.TimeInForce,
		ReduceOnly:            intent.Input.ReduceOnly,
		State:                 intent.State,
		CancelState:           intent.CancelState,
		ConfirmedCoreID:       intent.CoreOrderID,
		ConfirmedCoreRevision: intent.CoreRevision,
		Revision:              intent.Revision,
		ErrorCode:             intent.ErrorCode,
		Deadline:              intent.Input.Deadline,
		CreatedAt:             intent.CreatedAt,
		UpdatedAt:             intent.UpdatedAt,
	}
	if core == nil {
		if intent.CoreOrderID != 0 {
			return domain.AdminOrder{}, domain.ErrAdminUnavailable
		}
		return result, nil
	}
	observation, err := coreOrder(core)
	if err != nil {
		return domain.AdminOrder{}, err
	}
	if observation.ConfigVersion != result.ConfigVersion || observation.Side != result.Side || !observation.Price.Equal(result.Price) || !observation.Quantity.Equal(result.Quantity) ||
		observation.TimeInForce != result.TimeInForce || observation.ReduceOnly != result.ReduceOnly || result.ConfirmedCoreID != 0 && result.ConfirmedCoreID != observation.ID || result.ConfirmedCoreRevision > observation.Revision {
		return domain.AdminOrder{}, domain.ErrAdminUnavailable
	}
	result.Core = &observation
	result.CoreUpdatedAt = time.UnixMilli(core.UpdateAt).UTC()
	return result, nil
}

var _ domain.AdminOrders = (*adminOrders)(nil)
