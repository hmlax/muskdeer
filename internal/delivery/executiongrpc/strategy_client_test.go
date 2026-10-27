package executiongrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyClientPinsWritesAndQueriesOriginalKeyWithoutReplaying(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "strategy-test")
	if err != nil {
		t.Fatal(err)
	}
	input := domain.OrderInput{
		IdempotencyKey: "job-1",
		Authority: domain.StrategyAuthority{
			StrategyID:    1,
			ConfigVersion: 1,
			OwnerID:       owner,
			OwnerEpoch:    1,
		},
		AccountID:    "account",
		InstrumentID: 1,
		Side:         domain.SideBuy,
		Price:        decimal.RequireFromString("99.123456789012345678"),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
		Deadline:     time.Now().Add(500 * time.Millisecond),
	}
	wire := &strategyConnectionProbe{reply: strategyReplyFixture(input)}
	factory := &strategyFactoryProbe{connection: wire}
	routing := &strategyRouteProbe{instance: "execution.owner"}
	client, err := newStrategyClient(factory, routing)
	if err != nil {
		t.Fatal(err)
	}
	if len(factory.targets) != 1 || routing.calls != 0 || wire.calls != 0 {
		t.Fatal("client construction discovered or sent an operation")
	}
	order, err := client.Submit(ctx, input)
	if err != nil || order.Input.Price.String() != input.Price.String() || wire.submit.Price != input.Price.String() || wire.calls != 1 || routing.calls != 1 {
		t.Fatalf("typed submission changed identity or precision: %+v, %v", order, err)
	}
	if target := factory.targets[1]; target.Service != rpcsecurity.ServiceExecution || target.InstanceID != routing.instance {
		t.Fatal("write was not pinned to the current account owner")
	}
	if order.Input.Authority.OwnerID != "" || order.ExecutionAuthority.OwnerID != "" {
		t.Fatal("remote observation manufactured authority")
	}
	lost := errors.New("uncertain transport outcome")
	wire.failure = lost
	if _, err := client.Submit(ctx, input); !errors.Is(err, lost) || !errors.Is(err, domain.ErrOutcomeUnknown) || wire.calls != 2 {
		t.Fatal("uncertain write was replayed or its error was hidden")
	}
	wire.failure = nil
	observed, err := client.FindOrder(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || observed.ID != order.ID || wire.find.GetClientOrderId() != order.ClientOrderID || wire.calls != 3 || routing.calls != 2 {
		t.Fatalf("query did not retain original identity or unnecessarily acquire a writer: %+v, %v", observed, err)
	}
	unsafe := input
	unsafe.Price = decimal.New(1, 1_000_000_000)
	if _, err := client.Submit(ctx, unsafe); !errors.Is(err, domain.ErrInvalidOrder) || wire.calls != 3 || routing.calls != 2 {
		t.Fatal("hostile typed exponent reached formatting, routing or transport")
	}
	routing.failure = domain.ErrLeaseLost
	if _, err := client.Submit(ctx, input); !errors.Is(err, domain.ErrLeaseLost) || wire.calls != 3 {
		t.Fatal("missing owner fell back to another endpoint")
	}
	routing.failure = nil
	wire.reply.Order.AccountId = "another-account"
	if _, err := client.FindOrder(ctx, input.AccountID, input.IdempotencyKey); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("response crossed account scope")
	}
	assertUncertainInvalidStrategyReply(ctx, t, client, input)
}

func assertUncertainInvalidStrategyReply(ctx context.Context, t *testing.T, client domain.StrategyExecution, input domain.OrderInput) {
	t.Helper()
	if _, err := client.Submit(ctx, input); !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, domain.ErrInvalidOrder) || err.Error() != domain.MessageOutcomeUnknown {
		t.Fatal("an invalid reply after sending was incorrectly reported as a known no-effect outcome")
	}
}

func strategyReplyFixture(input domain.OrderInput) *executionv1.OrderReply {
	clientID, _ := domain.IntentClientOrderID(input.AccountID, input.IdempotencyKey)
	now := timestamppb.Now()
	return &executionv1.OrderReply{Order: &executionv1.OrderView{
		IntentId:           1,
		ClientOrderId:      clientID,
		CoreOrderId:        "1",
		CoreRevision:       1,
		ExecutionVenue:     domain.ExecutionVenueSimulator,
		AccountId:          input.AccountID,
		InstrumentId:       input.InstrumentID,
		StrategyInstanceId: input.Authority.StrategyID,
		ConfigVersion:      input.Authority.ConfigVersion,
		Side:               commonv1.OrderSide_ORDER_SIDE_BUY,
		Price:              input.Price.String(),
		Quantity:           input.Quantity.String(),
		FilledQuantity:     "0",
		Notional:           input.Price.Mul(input.Quantity).String(),
		TimeInForce:        commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY,
		State:              commonv1.OrderState_ORDER_STATE_LIVE,
		CancelState:        commonv1.CancelState_CANCEL_STATE_NONE,
		Revision:           1,
		UpdatedAt:          now,
		CreatedAt:          proto.CloneOf(now),
	}}
}

type strategyFactoryProbe struct {
	connection *strategyConnectionProbe
	targets    []rpcclient.Target
}

func (f *strategyFactoryProbe) Client(target rpcclient.Target) (grpc.ClientConnInterface, error) {
	f.targets = append(f.targets, target)
	return f.connection, nil
}

type strategyRouteProbe struct {
	instance string
	calls    int
	failure  error
}

func (r *strategyRouteProbe) ExecutionInstance(context.Context, string) (string, error) {
	r.calls++
	return r.instance, r.failure
}

type strategyConnectionProbe struct {
	reply            *executionv1.OrderReply
	submit           *executionv1.SubmitIntentRequest
	find             *executionv1.FindOrderRequest
	cancel           *executionv1.CancelOrderRequest
	cancellationFind *executionv1.FindCancellationRequest
	calls            int
	failure          error
}

func (c *strategyConnectionProbe) Invoke(_ context.Context, _ string, input, output any, _ ...grpc.CallOption) error {
	c.calls++
	if c.failure != nil {
		return c.failure
	}
	switch request := input.(type) {
	case *executionv1.SubmitIntentRequest:
		c.submit = proto.CloneOf(request)
	case *executionv1.FindOrderRequest:
		c.find = proto.CloneOf(request)
	case *executionv1.CancelOrderRequest:
		c.cancel = proto.CloneOf(request)
	case *executionv1.FindCancellationRequest:
		c.cancellationFind = proto.CloneOf(request)
	}
	proto.Merge(output.(*executionv1.OrderReply), c.reply)
	return nil
}

func TestStrategyClientRetainsCancellationIdentityAndSeparatesPendingFromTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	input := domain.OrderInput{
		IdempotencyKey: "order-key",
		AccountID:      "account",
		InstrumentID:   1,
		Authority: domain.StrategyAuthority{
			StrategyID:    1,
			ConfigVersion: 1,
		},
		Price:    decimal.NewFromInt(99),
		Quantity: decimal.NewFromInt(1),
	}
	reply := strategyReplyFixture(input)
	reply.Order.CancelState = commonv1.CancelState_CANCEL_STATE_PENDING
	reply.Cancellation = &executionv1.CancellationView{
		Id:             2,
		IntentId:       1,
		Revision:       1,
		IdempotencyKey: "cancel-key",
		State:          commonv1.CancellationState_CANCELLATION_STATE_PENDING,
	}
	wire := &strategyConnectionProbe{reply: reply}
	factory := &strategyFactoryProbe{connection: wire}
	routing := &strategyRouteProbe{instance: "execution.owner"}
	client, err := newStrategyClient(factory, routing)
	if err != nil {
		t.Fatal(err)
	}
	control := domain.CancelInput{
		IdempotencyKey: "cancel-key",
		AccountID:      "account",
		InstrumentID:   1,
		ClientOrderID:  reply.Order.ClientOrderId,
		Authority: domain.StrategyAuthority{
			StrategyID:    1,
			ConfigVersion: 1,
			OwnerID:       "strategy.01234567890123456789012345678901",
			OwnerEpoch:    1,
		},
		Deadline: time.Now().Add(500 * time.Millisecond),
	}
	pending, err := client.Cancel(ctx, control)
	if err != nil || pending.Request.State != domain.CancellationPending || pending.Order.State != domain.OrderStateLive ||
		pending.Request.Input.Authority.OwnerID != "" || wire.cancel.IdempotencyKey != control.IdempotencyKey {
		t.Fatalf("cancellation acceptance became terminal or fabricated authority: %+v, %v", pending, err)
	}
	reply.Order.State = commonv1.OrderState_ORDER_STATE_CANCELED
	reply.Order.CancelState = commonv1.CancelState_CANCEL_STATE_CONFIRMED
	reply.Order.CoreRevision, reply.Order.Revision = 2, 2
	reply.Cancellation.State = commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED
	routing.failure = domain.ErrLeaseLost
	confirmed, err := client.FindCancellation(ctx, control.AccountID, control.IdempotencyKey)
	if err != nil || confirmed.Request.State != domain.CancellationConfirmed || confirmed.Order.State != domain.OrderStateCanceled ||
		wire.cancellationFind.CancellationId != 0 || wire.cancellationFind.IdempotencyKey != control.IdempotencyKey || routing.calls != 1 || wire.calls != 2 {
		t.Fatalf("read recovery required former ownership or changed the key: %+v, %v", confirmed, err)
	}
	for _, mutate := range []func(*executionv1.OrderReply){
		func(v *executionv1.OrderReply) { v.Cancellation = nil },
		func(v *executionv1.OrderReply) { v.Cancellation.IdempotencyKey = "another-key" },
		func(v *executionv1.OrderReply) { v.Cancellation.IntentId = 2 },
		func(v *executionv1.OrderReply) {
			v.Cancellation.State = commonv1.CancellationState_CANCELLATION_STATE_UNSPECIFIED
		},
		func(v *executionv1.OrderReply) { v.Cancellation.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	} {
		changed := proto.CloneOf(reply)
		mutate(changed)
		if _, err := strategyCancellationObservation(changed, control.AccountID, control.IdempotencyKey); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatal("cancellation identity or enum mismatch accepted")
		}
	}
}

func (*strategyConnectionProbe) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, domain.ErrUnsupportedOrder
}
