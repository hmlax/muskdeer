package orderwire

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestOrderViewPreservesExactStringsAndUnknownState(t *testing.T) {
	value := decimal.RequireFromString("99999999999999999999.123456789012345678")
	snapshot := domain.IntentSnapshot{
		ID:            1,
		ClientOrderID: "stable-client",
		Input: domain.OrderInput{
			AccountID:    "account",
			InstrumentID: 1,
			Authority: domain.StrategyAuthority{
				StrategyID:    1,
				ConfigVersion: 1,
			},
			Side:        domain.SideBuy,
			Price:       value,
			Quantity:    value,
			TimeInForce: domain.TimeInForcePostOnly,
		},
		State:       domain.OrderStateUnknown,
		CancelState: domain.CancelStateNone,
		Revision:    1,
		UpdatedAt:   time.UnixMilli(2).UTC(),
		CreatedAt:   time.UnixMilli(1).UTC(),
	}
	view, err := Order(snapshot)
	if err != nil || view.Price != value.String() || view.Quantity != value.String() || view.Notional != value.Mul(value).String() || view.State != commonv1.OrderState_ORDER_STATE_UNKNOWN || view.CoreOrderId != "" {
		t.Fatalf("order view lost precision or fabricated an outcome: %v, %v", view, err)
	}
	observation, err := Snapshot(view)
	if err != nil || !observation.CreatedAt.Equal(snapshot.CreatedAt) || !observation.UpdatedAt.Equal(snapshot.UpdatedAt) || observation.CreatedAt.Equal(observation.UpdatedAt) {
		t.Fatalf("order observation lost immutable creation time: %+v, %v", observation, err)
	}
	data, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded executionv1.OrderView
	if err := protojson.Unmarshal(data, &decoded); err != nil || !proto.Equal(view, &decoded) {
		t.Fatalf("typed JSON response changed: %v", err)
	}
	snapshot.State = "invented"
	if _, err := Order(snapshot); err == nil {
		t.Fatal("unknown business state silently encoded")
	}
}
