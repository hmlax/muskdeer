package orderwire

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestObservedSnapshotRequiresConsistentTypedIdentityAndExactMoney(t *testing.T) {
	view := &executionv1.OrderView{
		IntentId:           1,
		ClientOrderId:      "client",
		AccountId:          "account",
		ExecutionVenue:     domain.ExecutionVenueSimulator,
		InstrumentId:       2,
		StrategyInstanceId: 3,
		ConfigVersion:      1,
		State:              commonv1.OrderState_ORDER_STATE_UNKNOWN,
		CancelState:        commonv1.CancelState_CANCEL_STATE_NONE,
		Side:               commonv1.OrderSide_ORDER_SIDE_BUY,
		TimeInForce:        commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY,
		Price:              "99.123456789012345678",
		Quantity:           "1",
		FilledQuantity:     "0",
		Notional:           "99.123456789012345678",
		Revision:           1,
		UpdatedAt:          timestamppb.New(time.UnixMilli(1)),
	}
	observed, err := Snapshot(view)
	if err != nil || observed.State != domain.OrderStateUnknown || observed.Input.Authority.OwnerID != "" || observed.CoreOrderID != 0 || observed.Input.Price.String() != view.Price {
		t.Fatalf("observed snapshot manufactured an authority or changed money: %+v, %v", observed, err)
	}
	for _, mutate := range []func(*executionv1.OrderView){
		func(v *executionv1.OrderView) { v.Notional = "1" },
		func(v *executionv1.OrderView) { v.Quantity = "1e2" },
		func(v *executionv1.OrderView) { v.State = commonv1.OrderState_ORDER_STATE_UNSPECIFIED },
		func(v *executionv1.OrderView) { v.CoreOrderId = "1.0" },
		func(v *executionv1.OrderView) { v.CoreOrderId = "001" },
		func(v *executionv1.OrderView) { v.AccountId = "" },
		func(v *executionv1.OrderView) { v.FilledQuantity = "2" },
		func(v *executionv1.OrderView) { v.UpdatedAt = nil },
		func(v *executionv1.OrderView) { v.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	} {
		changed := proto.CloneOf(view)
		mutate(changed)
		if _, err := Snapshot(changed); err == nil {
			t.Fatal("malformed observation accepted")
		}
	}
}
