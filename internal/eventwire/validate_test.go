package eventwire

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestPublicationRejectsChangedEnvelopeOrDecimalShape(t *testing.T) {
	event := &commonv1.ExecutionEvent{
		EventId:          "intent:1:1",
		EventType:        domain.EventOrderIntentAccepted,
		SchemaVersion:    domain.EventSchemaVersion,
		OccurredAtMs:     1,
		Producer:         domain.EventProducerExecution,
		AggregateId:      1,
		AggregateVersion: 1,
		Intent: &commonv1.IntentEvent{
			IntentId:              1,
			Revision:              1,
			StrategyInstanceId:    1,
			InstrumentId:          1,
			ConfigVersion:         1,
			AccountId:             "account-1",
			ClientOrderId:         "order-1",
			Side:                  commonv1.OrderSide_ORDER_SIDE_BUY,
			State:                 commonv1.OrderState_ORDER_STATE_INTENT,
			Price:                 "99.123456789012345678",
			Quantity:              "1",
			ReservedQuoteNotional: "99.123456789012345678",
		},
	}
	if err := Validate(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*commonv1.ExecutionEvent){
		func(e *commonv1.ExecutionEvent) { e.Intent.Price = "1e2" },
		func(e *commonv1.ExecutionEvent) { e.Intent.Quantity = "0" },
		func(e *commonv1.ExecutionEvent) { e.Intent.ReservedQuoteNotional = "NaN" },
		func(e *commonv1.ExecutionEvent) { e.Intent.FilledQuantity = "-1" },
		func(e *commonv1.ExecutionEvent) { e.Intent.Revision++ },
		func(e *commonv1.ExecutionEvent) { e.Intent.State = commonv1.OrderState_ORDER_STATE_UNSPECIFIED },
		func(e *commonv1.ExecutionEvent) { e.Cancellation = &commonv1.CancellationEvent{} },
		func(e *commonv1.ExecutionEvent) { e.Producer = "untrusted" },
		func(e *commonv1.ExecutionEvent) { e.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	} {
		changed := proto.Clone(event).(*commonv1.ExecutionEvent)
		mutate(changed)
		if err := Validate(t.Context(), changed); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("invalid publication accepted: %v", err)
		}
	}
}
