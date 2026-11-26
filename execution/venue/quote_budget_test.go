package venue

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func waitQuoteOwners(t *testing.T, budget *quoteSendBudget, count int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		budget.mu.Lock()
		got := len(budget.waiting)
		budget.mu.Unlock()
		if got == count {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("quote owner did not reach the capacity wait")
}

func TestQuoteBudgetFairOwnersCannotBeBypassedByExtraWaveSends(t *testing.T) {
	budget := newQuoteSendBudget(2)
	first, err := budget.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := budget.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	third := make(chan *quoteSendPermit, 1)
	go func() {
		permit, err := budget.acquire(t.Context())
		if err != nil {
			third <- nil
			return
		}
		third <- permit
	}()
	waitQuoteOwners(t, budget, 1)
	ctx, cancel := context.WithCancel(t.Context())
	fourth := make(chan error, 1)
	go func() { _, err := budget.acquire(ctx); fourth <- err }()
	waitQuoteOwners(t, budget, 2)
	if extra := first.borrow(3); extra != 0 {
		t.Fatalf("wave bypassed queued owners with %d extra permits", extra)
	}
	cancel()
	if err := <-fourth; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first.release()
	first.release() // Release remains idempotent during cancellation cleanup.
	permit := <-third
	if permit == nil {
		t.Fatal("oldest live owner did not receive the released permit")
	}
	permit.release()
	second.release()
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.active != 0 || len(budget.waiting) != 0 {
		t.Fatalf("admission leaked capacity: active=%d waiting=%d", budget.active, len(budget.waiting))
	}
}

func TestQuoteMailboxSelectsLatestIntentAndStartsDeadlineAfterOwnerAdmission(t *testing.T) {
	budget := newQuoteSendBudget(1)
	busy, err := budget.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer busy.release()
	mailbox := newQuoteMailbox()
	mailbox.offer(mailboxTestPlan(1))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := errors.New("observed latest intent")
	done := make(chan error, 1)
	go func() {
		done <- mailbox.runAdmitted(ctx, time.Second, func(ctx context.Context) (context.Context, func(), error) {
			permit, err := budget.acquire(ctx)
			if err != nil {
				return ctx, nil, err
			}
			return withQuoteSendPermit(ctx, permit), permit.release, nil
		}, func(work context.Context, plan *pb.QuotePlan) error {
			if plan.GetBook().GetSequence() != 100 {
				return errors.New("selected stale intent before admission")
			}
			deadline, ok := work.Deadline()
			if !ok || time.Until(deadline) < 900*time.Millisecond {
				return errors.New("capacity wait consumed the fresh operation deadline")
			}
			if quoteSendPermitFrom(work) == nil {
				return errors.New("worker lost its reserved SDK capacity")
			}
			return stop
		})
	}()
	waitQuoteOwners(t, budget, 1)
	for n := int64(2); n <= 100; n++ {
		mailbox.offer(mailboxTestPlan(n))
	}
	time.Sleep(110 * time.Millisecond) // A deadline created before admission would now have <900ms.
	busy.release()
	if err := <-done; !errors.Is(err, stop) {
		t.Fatal(err)
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.active != 0 || len(budget.waiting) != 0 {
		t.Fatal("completed mailbox retained its admission")
	}
}

func TestFairQuoteOwnersKeepGlobalSDKBoundAndJoinEveryWave(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	budget := newQuoteSendBudget(2)
	client := &heldCancelClient{
		started: make(chan uint64, 60),
		release: make(chan struct{}),
	}
	done := make(chan error, 20)
	var owners sync.WaitGroup
	for fund := range 20 {
		owners.Add(1)
		go func() {
			defer owners.Done()
			permit, err := budget.acquire(ctx)
			if err != nil {
				done <- err
				return
			}
			defer permit.release()
			work := withQuoteSendPermit(ctx, permit)
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: int64(fund + 1),
					Template:   domain.StrategyTemplateMarketMaker,
				},
				client:      &atomic.Pointer[gatewaySession]{},
				quoteBudget: budget,
			}
			runtime.client.Store(&gatewaySession{Client: client})
			var orders []*common.VenueOrder
			for number := range 3 {
				orders = append(orders, &common.VenueOrder{OrderId: strconv.Itoa(fund*3 + number + 1)})
			}
			result := &pb.QuoteResult{}
			err = runtime.cancelOrders(work, domain.LeaseToken{}, &common.VenueProfilePolicy{}, orders, result)
			if err == nil && result.Cancelled != 3 {
				err = errors.New("fund did not complete its distinct cancellation wave")
			}
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-client.started:
		case <-ctx.Done():
			t.Fatal("reserved owners failed to start SDK work")
		}
	}
	if client.active.Load() != 2 || client.maximum.Load() > 2 {
		t.Fatal("global send capacity was not respected")
	}
	close(client.release)
	for range 20 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	owners.Wait()
	if client.active.Load() != 0 || client.calls.Load() != 60 || client.maximum.Load() > 2 {
		t.Fatalf("SDK sends active=%d calls=%d max=%d", client.active.Load(), client.calls.Load(), client.maximum.Load())
	}
}

func TestReservedQuoteWaveStopsUnsentSiblingsAfterAmbiguousFailure(t *testing.T) {
	budget := newQuoteSendBudget(1)
	permit, err := budget.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer permit.release()
	failure := errors.New("ambiguous SDK cancellation")
	client := &heldCancelClient{
		started: make(chan uint64, 3),
		release: make(chan struct{}),
		failure: failure,
	}
	runtime := &Runtime{
		cfg: &venueconfig.RuntimeConfig{
			InstanceID: 1,
			Template:   domain.StrategyTemplateMarketMaker,
		},
		client:      &atomic.Pointer[gatewaySession]{},
		quoteBudget: budget,
	}
	runtime.client.Store(&gatewaySession{Client: client})
	orders := []*common.VenueOrder{{OrderId: "1"}, {OrderId: "2"}, {OrderId: "3"}}
	result := &pb.QuoteResult{}
	err = runtime.cancelOrders(withQuoteSendPermit(t.Context(), permit), domain.LeaseToken{}, &common.VenueProfilePolicy{}, orders, result)
	if !errors.Is(err, failure) || client.calls.Load() != 1 || client.active.Load() != 0 || result.Cancelled != 0 {
		t.Fatalf("ambiguous wave err=%v calls=%d active=%d success=%d", err, client.calls.Load(), client.active.Load(), result.Cancelled)
	}
}

func TestQuoteBudgetCancelledOwnerNeverConsumesReleasedCapacity(t *testing.T) {
	for range 100 {
		budget := newQuoteSendBudget(1)
		first, err := budget.acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			permit, err := budget.acquire(ctx)
			if permit != nil {
				permit.release()
			}
			done <- err
		}()
		waitQuoteOwners(t, budget, 1)
		cancel()
		first.release()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter result=%v", err)
		}
		budget.mu.Lock()
		active, queued := budget.active, len(budget.waiting)
		budget.mu.Unlock()
		if active != 0 || queued != 0 {
			t.Fatalf("cancel/grant race leaked capacity: %d/%d", active, queued)
		}
	}
}
