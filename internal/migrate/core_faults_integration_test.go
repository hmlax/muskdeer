//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/simulator"
)

func TestCommittedCoreResponseLossLeavesOneQueryableOrderAndPendingCancel(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	gateway, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit: simulator.ResponseLost,
		Cancel: simulator.ResponseLost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if order, err := gateway.Submit(ctx, submit); !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) || order.ID != 0 {
		t.Fatalf("committed lost reply = %+v, %v", order, err)
	}
	found, err := gateway.Find(ctx, submit.Input.AccountID, submit.ClientOrderID)
	if err != nil || found.State != domain.OrderStateLive {
		t.Fatalf("unknown order query = %+v, %v", found, err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	if replayed, err := core.Submit(ctx, submit); err != nil || replayed.ID != found.ID {
		t.Fatalf("explicit same-identity replay = %+v, %v", replayed, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	cancellation := domain.GatewayCancel{
		Input: domain.CancelInput{
			IdempotencyKey: "lost-cancel-1",
			Authority:      submit.Input.Authority,
			AccountID:      submit.Input.AccountID,
			InstrumentID:   submit.Input.InstrumentID,
			ClientOrderID:  submit.ClientOrderID,
			Deadline:       submit.Input.Deadline,
		},
		ExecutionAuthority: submit.ExecutionAuthority,
	}
	if _, err := gateway.Cancel(ctx, cancellation); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("cancel lost reply = %v", err)
	}
	found, err = gateway.Find(ctx, submit.Input.AccountID, submit.ClientOrderID)
	if err != nil || found.CancelState != domain.CancelStatePending || found.State != domain.OrderStateLive {
		t.Fatalf("cancel query pretended terminal completion: %+v, %v", found, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, submit.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}

func TestDelayedCoreReplyCanExpireAfterOrderCommits(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	committed, gate := make(chan struct{}, 1), make(chan struct{})
	gateway, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit:    simulator.ResponseWait,
		Cancel:    simulator.ResponseNormal,
		Committed: committed,
		Gate:      gate,
	})
	if err != nil {
		t.Fatal(err)
	}
	callCtx, stop := context.WithCancelCause(ctx)
	result := make(chan error, 1)
	var workers sync.WaitGroup
	t.Cleanup(func() {
		stop(nil)
		workers.Wait()
	})
	workers.Go(func() {
		_, err := gateway.Submit(callCtx, submit)
		result <- err
	})
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("core did not reach its committed boundary")
	}
	if found, err := gateway.Find(ctx, submit.Input.AccountID, submit.ClientOrderID); err != nil || found.State != domain.OrderStateLive {
		t.Fatalf("delayed committed order cannot be queried: %+v, %v", found, err)
	}
	stop(context.DeadlineExceeded)
	select {
	case err := <-result:
		if !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("delayed commit lost uncertainty: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("delayed response worker did not exit")
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
}
