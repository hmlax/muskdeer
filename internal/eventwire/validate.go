package eventwire

import (
	"context"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Validate checks all currently produced variants before broker publication.
func Validate(ctx context.Context, event *commonv1.ExecutionEvent) error {
	if ctx == nil {
		return ErrInvalidEvent
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if event == nil || event.SchemaVersion != domain.EventSchemaVersion || event.Producer != domain.EventProducerExecution ||
		event.OccurredAtMs <= 0 || event.AggregateId <= 0 || event.AggregateVersion <= 0 || !identifier(event.EventId, 128) ||
		proto.Size(event) > 64*1024 || len(event.ProtoReflect().GetUnknown()) != 0 {
		return ErrInvalidEvent
	}
	switch event.EventType {
	case domain.EventTradeFilled:
		_, err := Fill(ctx, event)
		return err
	case domain.EventOrderIntentAccepted, domain.EventOrderStateChanged:
		return validateIntent(event)
	case domain.EventOrderCancelRequested, domain.EventOrderCancelStateChanged:
		return validateCancellation(event)
	default:
		return ErrInvalidEvent
	}
}

func validateIntent(event *commonv1.ExecutionEvent) error {
	in := event.Intent
	if in == nil || event.Fill != nil || event.Cancellation != nil || len(in.ProtoReflect().GetUnknown()) != 0 ||
		in.IntentId != event.AggregateId || in.Revision != event.AggregateVersion || in.StrategyInstanceId <= 0 || in.InstrumentId <= 0 || in.ConfigVersion <= 0 ||
		!identifier(in.AccountId, 64) || !identifier(in.ClientOrderId, 64) ||
		(in.Side != commonv1.OrderSide_ORDER_SIDE_BUY && in.Side != commonv1.OrderSide_ORDER_SIDE_SELL) ||
		in.State < commonv1.OrderState_ORDER_STATE_INTENT || in.State > commonv1.OrderState_ORDER_STATE_UNKNOWN ||
		in.CoreOrderId < 0 || in.CoreRevision < 0 {
		return ErrInvalidEvent
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	if _, err := domain.ParseDecimal(in.Price, rule); err != nil {
		return ErrInvalidEvent
	}
	if _, err := domain.ParseDecimal(in.Quantity, rule); err != nil {
		return ErrInvalidEvent
	}
	if _, err := domain.ParseLedgerDecimal(in.ReservedQuoteNotional, false, true); err != nil {
		return ErrInvalidEvent
	}
	if in.FilledQuantity != "" {
		rule.AllowZero = true
		if _, err := domain.ParseDecimal(in.FilledQuantity, rule); err != nil {
			return ErrInvalidEvent
		}
	}
	if in.CancelState < commonv1.CancelState_CANCEL_STATE_UNSPECIFIED || in.CancelState > commonv1.CancelState_CANCEL_STATE_REJECTED {
		return ErrInvalidEvent
	}
	return nil
}

func validateCancellation(event *commonv1.ExecutionEvent) error {
	in := event.Cancellation
	if in == nil || event.Fill != nil || event.Intent != nil || len(in.ProtoReflect().GetUnknown()) != 0 ||
		in.CancellationId != event.AggregateId || in.Revision != event.AggregateVersion || in.IntentId <= 0 || in.StrategyInstanceId <= 0 ||
		!identifier(in.AccountId, 64) || !identifier(in.ClientOrderId, 64) ||
		in.State < commonv1.CancellationState_CANCELLATION_STATE_REQUESTED || in.State > commonv1.CancellationState_CANCELLATION_STATE_REJECTED {
		return ErrInvalidEvent
	}
	return nil
}
