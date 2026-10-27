package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
)

func appendSimulatorFillEvent(ctx context.Context, tx *gorm.DB, order *modelv1.SimulatorOrder, fill *modelv1.SimulatorFill) error {
	var command modelv1.SimulatorCommand
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("id = ? AND delete_at IS NULL", order.InitialCommandId).First(&command).Error; err != nil {
		return err
	}
	if command.IntentId == nil {
		// Privileged direct simulator scenarios have no OMS aggregate. The
		// command's immutable original linkage decides this, not a later lookup.
		return nil
	}
	now, err := businessClock(ctx, tx)
	if err != nil {
		return err
	}
	event, err := simulatorFillEvent(ctx, order, fill, *command.IntentId, now)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Create(&modelv1.OutboxEvent{
		EventId:       event.EventId,
		EventType:     event.EventType,
		IntentId:      *command.IntentId,
		Event:         event,
		State:         domain.OutboxStatePending,
		NextAttemptAt: now,
	}).Error
}

func simulatorFillEvent(ctx context.Context, order *modelv1.SimulatorOrder, fill *modelv1.SimulatorFill, intentID, now int64) (*commonv1.ExecutionEvent, error) {
	if order == nil || fill == nil || fill.CoreOrderId != order.Id || (order.Side != string(domain.SideBuy) && order.Side != string(domain.SideSell)) {
		return nil, domain.ErrInvalidOrder
	}
	side := commonv1.OrderSide_ORDER_SIDE_BUY
	if order.Side == string(domain.SideSell) {
		side = commonv1.OrderSide_ORDER_SIDE_SELL
	}
	event := &commonv1.ExecutionEvent{
		EventId:          fmt.Sprintf("fill:simulator:%d", fill.Id),
		EventType:        domain.EventTradeFilled,
		SchemaVersion:    domain.EventSchemaVersion,
		OccurredAtMs:     now,
		Producer:         domain.EventProducerExecution,
		AggregateId:      fill.Id,
		AggregateVersion: 1,
		RequestId:        requestid.FromContext(ctx).String(),
		Fill: &commonv1.FillEvent{
			TradeId:            fill.TradeId,
			ExecutionVenue:     domain.ExecutionVenueSimulator,
			CoreOrderId:        order.Id,
			IntentId:           intentID,
			AccountId:          order.AccountId,
			StrategyInstanceId: order.StrategyInstanceId,
			InstrumentId:       order.InstrumentId,
			Side:               side,
			Price:              fill.Price,
			Quantity:           fill.Quantity,
			QuoteNotional:      fill.QuoteNotional,
			FeeAmount:          fill.FeeAmount,
			FeeAsset:           fill.FeeAsset,
			SourceSequence:     fill.Id,
		},
	}
	if err := eventwire.Validate(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}
