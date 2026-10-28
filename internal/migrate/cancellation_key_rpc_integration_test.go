//go:build integration

package migrate

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/simulator"
)

func TestCancellationRPCRecoversLostReplyByStableKeyWithoutAnotherCancel(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	committed := make(chan struct{}, 1)
	lost, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit:    simulator.ResponseNormal,
		Cancel:    simulator.ResponseLost,
		Committed: committed,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := &observedCancellationGateway{TradingGateway: lost}
	writer, reader := executionRPCFixture(ctx, t, db, observed, intents, submit.ExecutionAuthority.OwnerID)
	if reply, err := reader.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: submit.Input.AccountID,
		Lookup:    &executionv1.FindOrderRequest_IntentId{IntentId: live.ID},
	}); err != nil || reply.GetOrder().GetState() != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatal("authenticated control did not observe the original live order")
	}
	key := strings.Repeat("k", 128)
	if reply, err := writer.CancelOrder(ctx, &executionv1.CancelOrderRequest{
		IdempotencyKey: key,
		Authority:      rpcAuthority(submit.Input.Authority),
		AccountId:      submit.Input.AccountID,
		InstrumentId:   submit.Input.InstrumentID,
		ClientOrderId:  live.ClientOrderID,
		Deadline:       timestamppb.New(time.Now().Add(time.Second)),
	}); err != nil || reply.GetCancellation().GetState() != commonv1.CancellationState_CANCELLATION_STATE_UNKNOWN || observed.cancels.Load() != 1 {
		t.Fatalf("lost venue cancellation did not retain uncertainty: %v, %v", reply, err)
	}
	select {
	case <-committed:
	default:
		t.Fatal("cancellation never reached the intended committed-response-loss boundary")
	}
	// Discard the first reply's server ID. The caller retains only its durable
	// account/key pair, even after both former owners have lost authority.
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	})
	lookup := &executionv1.FindCancellationRequest{
		AccountId:      submit.Input.AccountID,
		IdempotencyKey: key,
	}
	pending, err := reader.FindCancellation(ctx, lookup)
	if err != nil || pending.GetCancellation().GetState() != commonv1.CancellationState_CANCELLATION_STATE_PENDING || pending.GetOrder().GetIntentId() != live.ID {
		t.Fatalf("original cancellation key did not recover pending outcome: %v, %v", pending, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := reader.FindCancellation(ctx, lookup)
	if err != nil || confirmed.GetCancellation().GetState() != commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED || confirmed.GetCancellation().GetIdempotencyKey() != key || confirmed.GetOrder().GetState() != commonv1.OrderState_ORDER_STATE_CANCELED || observed.cancels.Load() != 1 {
		t.Fatalf("stable-key observation resent or lost terminal identity: %v, %v", confirmed, err)
	}
	for _, request := range []*executionv1.FindCancellationRequest{
		{AccountId: submit.Input.AccountID},
		{
			AccountId:      submit.Input.AccountID,
			CancellationId: confirmed.GetCancellation().GetId(),
			IdempotencyKey: key,
		},
		{
			AccountId:      submit.Input.AccountID,
			CancellationId: -1,
			IdempotencyKey: key,
		},
		{
			AccountId:      submit.Input.AccountID,
			IdempotencyKey: strings.Repeat("x", 129),
		},
		{
			AccountId:      submit.Input.AccountID,
			IdempotencyKey: "invalid key",
		},
	} {
		if _, err := reader.FindCancellation(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ambiguous/malformed cancellation selector accepted: %v", err)
		}
	}
	for _, request := range []*executionv1.FindCancellationRequest{
		{
			AccountId:      "another-account",
			IdempotencyKey: key,
		},
		{
			AccountId:      submit.Input.AccountID,
			IdempotencyKey: "missing-key",
		},
	} {
		if _, err := reader.FindCancellation(ctx, request); status.Code(err) != codes.NotFound {
			t.Fatalf("missing or wrong-account key leaked another request: %v", err)
		}
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorCommand{}, 2)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

type observedCancellationGateway struct {
	domain.TradingGateway
	cancels atomic.Int32
}

func (g *observedCancellationGateway) Cancel(ctx context.Context, command domain.GatewayCancel) (domain.CoreOrder, error) {
	g.cancels.Add(1)
	return g.TradingGateway.Cancel(ctx, command)
}
