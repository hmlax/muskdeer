package simulator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/hmlax/muskdeer/internal/domain"
)

type faultCore struct {
	count atomic.Int64
	err   error
}

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func (f *faultCore) Submit(context.Context, domain.GatewaySubmit) (domain.CoreOrder, error) {
	f.count.Add(1)
	return domain.CoreOrder{ID: 17}, f.err
}

func (f *faultCore) Cancel(context.Context, domain.GatewayCancel) (domain.CoreOrder, error) {
	f.count.Add(1)
	return domain.CoreOrder{ID: 17}, f.err
}

func (*faultCore) Find(context.Context, string, string) (domain.CoreOrder, error) {
	return domain.CoreOrder{ID: 17}, nil
}

func (*faultCore) Balances(context.Context, string) ([]domain.CoreBalance, error) { return nil, nil }

func TestLostResponseDoesNotReplayAndKeepsQueryAvailable(t *testing.T) {
	core := &faultCore{}
	gateway, err := WithFaults(core, FaultPlan{
		Submit: ResponseLost,
		Cancel: ResponseLost,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	order, err := gateway.Submit(ctx, domain.GatewaySubmit{})
	if !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) || order.ID != 0 || core.count.Load() != 1 {
		t.Fatalf("response loss replayed or exposed success: %v, %v", order, err)
	}
	if found, err := gateway.Find(ctx, "account", "client"); err != nil || found.ID != 17 || core.count.Load() != 1 {
		t.Fatalf("query after loss = %v, %v", found, err)
	}
	if _, err := gateway.Cancel(ctx, domain.GatewayCancel{}); !errors.Is(err, domain.ErrOutcomeUnknown) || core.count.Load() != 2 {
		t.Fatalf("cancel response loss = %v", err)
	}
}

func TestDelayedCommittedReplyExitsOnCallerCancellation(t *testing.T) {
	core := &faultCore{}
	committed := make(chan struct{}, 1)
	gate := make(chan struct{})
	gateway, err := WithFaults(core, FaultPlan{
		Submit:    ResponseWait,
		Cancel:    ResponseNormal,
		Gate:      gate,
		Committed: committed,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	callCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	result := make(chan error, 1)
	var workers sync.WaitGroup
	t.Cleanup(func() {
		stop(nil)
		workers.Wait()
	})
	workers.Go(func() {
		_, err := gateway.Submit(callCtx, domain.GatewaySubmit{})
		result <- err
	})
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("did not reach the committed boundary")
	}
	stop(context.DeadlineExceeded)
	select {
	case err := <-result:
		if !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) || core.count.Load() != 1 {
			t.Fatalf("delayed reply did not preserve uncertainty/cause: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("delayed caller did not join")
	}
}

func TestConfirmedRejectionRemainsConfirmed(t *testing.T) {
	core := &faultCore{err: domain.ErrInsufficientFunds}
	gateway, err := WithFaults(core, FaultPlan{
		Submit: ResponseLost,
		Cancel: ResponseNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := gateway.Submit(ctx, domain.GatewaySubmit{}); !errors.Is(err, domain.ErrInsufficientFunds) || errors.Is(err, domain.ErrOutcomeUnknown) || core.count.Load() != 1 {
		t.Fatalf("confirmed rejection became ambiguous: %v", err)
	}
}

func TestReleasedReplyAndInvalidPlans(t *testing.T) {
	core := &faultCore{}
	if _, err := WithFaults(core, FaultPlan{}); err == nil {
		t.Fatal("empty response policy became an implicit default")
	}
	if _, err := WithFaults(core, FaultPlan{
		Submit: ResponseWait,
		Cancel: ResponseNormal,
	}); err == nil {
		t.Fatal("unbounded wait without a gate was accepted")
	}
	gate := make(chan struct{})
	close(gate)
	gateway, err := WithFaults(core, FaultPlan{
		Submit: ResponseWait,
		Cancel: ResponseNormal,
		Gate:   gate,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if order, err := gateway.Submit(ctx, domain.GatewaySubmit{}); err != nil || order.ID != 17 || core.count.Load() != 1 {
		t.Fatalf("released committed reply = %v, %v", order, err)
	}
}
