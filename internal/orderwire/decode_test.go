package orderwire

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestSubmitTypedContractPreservesDecimalAndTransportContext(t *testing.T) {
	now := time.Now().UTC()
	ctx, cancel := context.WithDeadline(t.Context(), now.Add(2*time.Second))
	defer cancel()
	id, err := requestid.New(ctx, "transport-correlation")
	if err != nil {
		t.Fatal(err)
	}
	ctx = requestid.Bind(ctx, id)
	input := orderFixture(now)
	data, err := protojson.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip executionv1.SubmitIntentRequest
	if err := protojson.Unmarshal(data, &roundtrip); err != nil || !proto.Equal(input, &roundtrip) {
		t.Fatalf("typed wire roundtrip changed intent: %v", err)
	}
	output, err := Submit(ctx, &roundtrip, now)
	if err != nil {
		t.Fatal(err)
	}
	if output.Price.String() != input.Price || output.Quantity.String() != input.Quantity || output.Price.Mul(output.Quantity).String() != "19999999999999999999.9999999999999999998" {
		t.Fatal("decimal intent arithmetic lost precision")
	}
	if output.RequestID != id.String() || output.IdempotencyKey != input.IdempotencyKey || output.Side != domain.SideBuy || output.TimeInForce != domain.TimeInForcePostOnly || output.Authority.OwnerEpoch != input.Authority.OwnerEpoch {
		t.Fatalf("typed intent identity changed: %v", output)
	}
	for _, change := range []func(*executionv1.SubmitIntentRequest){
		func(value *executionv1.SubmitIntentRequest) { value.Price = "1e9" },
		func(value *executionv1.SubmitIntentRequest) { value.Price = "0" },
		func(value *executionv1.SubmitIntentRequest) { value.Price = "0.0000000000000000001" },
		func(value *executionv1.SubmitIntentRequest) { value.Quantity = "NaN" },
		func(value *executionv1.SubmitIntentRequest) { value.Side = commonv1.OrderSide_ORDER_SIDE_UNSPECIFIED },
		func(value *executionv1.SubmitIntentRequest) {
			value.TimeInForce = commonv1.TimeInForce_TIME_IN_FORCE_UNSPECIFIED
		},
		func(value *executionv1.SubmitIntentRequest) { value.Authority = nil },
		func(value *executionv1.SubmitIntentRequest) { value.Authority.OwnerEpoch = 0 },
		func(value *executionv1.SubmitIntentRequest) { value.AccountId = "" },
		func(value *executionv1.SubmitIntentRequest) { value.IdempotencyKey = "" },
		func(value *executionv1.SubmitIntentRequest) { value.Deadline = nil },
		func(value *executionv1.SubmitIntentRequest) {
			value.Deadline = timestamppb.New(now.Add(-time.Millisecond))
		},
		func(value *executionv1.SubmitIntentRequest) {
			value.Deadline = timestamppb.New(now.Add(3 * time.Second))
		},
		func(value *executionv1.SubmitIntentRequest) { value.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1}) },
	} {
		changed := proto.CloneOf(input)
		change(changed)
		if _, err := Submit(ctx, changed, now); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatalf("invalid typed order was accepted: %v", err)
		}
	}
	if _, err := Submit(context.WithoutCancel(ctx), input, now); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("unbounded intent was accepted: %v", err)
	}
}

func TestCancelHasIndependentOrderIdentityAndBoundedLifetime(t *testing.T) {
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	order := orderFixture(now)
	input := &executionv1.CancelOrderRequest{
		RequestId:      "cancel-correlation",
		IdempotencyKey: "cancel-a",
		Authority:      proto.CloneOf(order.Authority),
		AccountId:      order.AccountId,
		InstrumentId:   order.InstrumentId,
		ClientOrderId:  "stable-client-order-a",
		Deadline:       timestamppb.New(now.Add(time.Second)),
	}
	output, err := Cancel(ctx, input, now)
	if err != nil || output.ClientOrderID != input.ClientOrderId || output.Authority.StrategyID != order.Authority.StrategyInstanceId {
		t.Fatalf("typed cancel = %v, %v", output, err)
	}
	invalidCancel := proto.CloneOf(input)
	invalidCancel.ClientOrderId = ""
	if _, err := Cancel(ctx, invalidCancel, now); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("missing cancel target = %v", err)
	}
	cancel()
	if _, err := Cancel(ctx, input, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request lost native cause: %v", err)
	}
	if _, err := Submit(ctx, order, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled intent lost native cause: %v", err)
	}
}

func orderFixture(now time.Time) *executionv1.SubmitIntentRequest {
	return &executionv1.SubmitIntentRequest{
		RequestId:      "body-correlation",
		IdempotencyKey: "intent-a",
		Authority: &executionv1.StrategyAuthority{
			StrategyInstanceId: 3,
			ConfigVersion:      2,
			OwnerId:            "strategy-a",
			OwnerEpoch:         7,
		},
		AccountId:    "account-a",
		InstrumentId: 4,
		Side:         commonv1.OrderSide_ORDER_SIDE_BUY,
		Price:        "99999999999999999999.999999999999999999",
		Quantity:     "0.2",
		TimeInForce:  commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY,
		Deadline:     timestamppb.New(now.Add(time.Second)),
	}
}
