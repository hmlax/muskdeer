//go:build integration

package venue

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type blockedQuoteClient struct {
	gateway.Client
	started    chan gateway.QuoteRequest
	release    chan struct{}
	active     atomic.Int64
	maximum    atomic.Int64
	calls      atomic.Int64
	firstError error
}

func (c *blockedQuoteClient) CancelQuote(context.Context, gateway.CancelRequest) error {
	return errors.New("unexpected cancellation during refill")
}

func (c *blockedQuoteClient) SubmitQuote(ctx context.Context, request gateway.QuoteRequest) (*gateway.SubmitResult, error) {
	id := c.calls.Add(1)
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for previous := c.maximum.Load(); active > previous; previous = c.maximum.Load() {
		if c.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	c.started <- request
	if id == 1 && c.firstError != nil {
		return nil, c.firstError
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.release:
		return &gateway.SubmitResult{OrderID: uint64(id)}, nil
	}
}

func TestMakerDepthRefillDoesNotWaitForEachAcknowledgement(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	seen := make(map[string]bool)
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for range 4 {
		select {
		case request := <-client.started:
			seen[request.ClientID] = true
		case <-timer.C:
			cancel()
			close(client.release)
			err := <-done
			t.Fatalf("only %d requests started before the first acknowledgement; refill is serial (joined: %v)", len(seen), err)
		}
	}
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for len(client.started) > 0 {
		request := <-client.started
		if seen[request.ClientID] {
			t.Fatal("a quote identity was dispatched more than once")
		}
		seen[request.ClientID] = true
	}
	if cycle.result.Placed != 8 || len(seen) != 8 || client.maximum.Load() != 4 || client.active.Load() != 0 {
		t.Fatalf("refill placed=%d identities=%d concurrent=%d active=%d", cycle.result.Placed, len(seen), client.maximum.Load(), client.active.Load())
	}
}

func TestMakerQuoteWaveReservesAggregateBudgetBeforeSending(t *testing.T) {
	runtime, cycle, client, setup := batchQuoteSetup(t.Context(), t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	close(client.release)
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Update("maximum_notional", "130").Error; err != nil {
		t.Fatal(err)
	}
	err := runtime.fillQuoteDepth(ctx, cycle)
	if !errors.Is(err, ErrRiskLimit) || client.calls.Load() != 0 {
		t.Fatalf("oversized wave error=%v dispatched=%d, want risk refusal before any send", err, client.calls.Load())
	}
}

func TestMakerQuoteWaveJoinsCancelledSiblings(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	for range 4 {
		select {
		case <-client.started:
		case <-ctx.Done():
			err := <-done
			t.Fatalf("wave failed to start within its deadline: %v", err)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || client.active.Load() != 0 || cycle.result.Placed != 0 || client.calls.Load() != 4 {
		t.Fatalf("cancelled wave err=%v active=%d placed=%d calls=%d", err, client.active.Load(), cycle.result.Placed, client.calls.Load())
	}
}

func TestMakerQuoteWaveJoinsPartialFailureWithoutRetry(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	failed := errors.New("ambiguous SDK outcome")
	client.firstError = failed
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	for range 4 {
		select {
		case <-client.started:
		case <-ctx.Done():
			err := <-done
			t.Fatalf("failed wave did not join before deadline: %v", err)
		}
	}
	select {
	case err := <-done:
		cancel()
		t.Fatalf("wave returned before its other requests completed: %v", err)
	default:
	}
	close(client.release)
	if err := <-done; !errors.Is(err, failed) || cycle.result.Placed != 3 || client.calls.Load() != 4 || client.active.Load() != 0 {
		t.Fatalf("partial wave err=%v placed=%d calls=%d active=%d", err, cycle.result.Placed, client.calls.Load(), client.active.Load())
	}
}

func TestMakerQuoteAdmissionCannotExtendSourceFreshness(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime.quoteSlots = make(chan struct{}, 1)
	runtime.quoteSlots <- struct{}{} // Other funds already occupy transport capacity.
	cycle.authority.Book.ValidUntilMs = time.Now().Add(650 * time.Millisecond).UnixMilli()
	err := runtime.fillQuoteDepth(ctx, cycle)
	if !errors.Is(err, context.DeadlineExceeded) || client.calls.Load() != 0 || len(runtime.quoteSlots) != 1 {
		t.Fatalf("expired queued wave err=%v calls=%d occupied=%d", err, client.calls.Load(), len(runtime.quoteSlots))
	}
}

func TestMakerQuoteWavesShareTransportBound(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime.quoteSlots = make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	for range 2 {
		select {
		case <-client.started:
		case <-ctx.Done():
			err := <-done
			t.Fatalf("transport failed to admit requests: %v", err)
		}
	}
	if client.calls.Load() != 2 {
		cancel()
		err := <-done
		t.Fatalf("transport bound exceeded while acknowledgements blocked: %v", err)
	}
	close(client.release)
	if err := <-done; err != nil || client.maximum.Load() > 2 || cycle.result.Placed != 8 || len(runtime.quoteSlots) != 0 {
		t.Fatalf("bounded wave err=%v concurrent=%d placed=%d occupied=%d", err, client.maximum.Load(), cycle.result.Placed, len(runtime.quoteSlots))
	}
}

func TestConfiguredQuoteWaveReturnsSDKCapacityBeforeFurtherObservation(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	runtime.quoteTurns = newQuoteSendBudget(2)
	runtime.quoteBudget = newQuoteSendBudget(2)
	runtime.cfg.OperationTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	work, releaseTurn, err := runtime.admitMakerTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseTurn()
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(work, cycle) }()
	for range 2 {
		select {
		case <-client.started:
		case <-ctx.Done():
			t.Fatal("ready configured wave failed to borrow idle SDK capacity")
		}
	}
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	runtime.quoteBudget.mu.Lock()
	active := runtime.quoteBudget.active
	runtime.quoteBudget.mu.Unlock()
	if active != 0 || cycle.result.Placed != 8 || client.maximum.Load() > 2 || client.calls.Load() != 8 {
		t.Fatalf("wave retained SDK capacity or changed dispatch: held=%d placed=%d max=%d calls=%d", active, cycle.result.Placed, client.maximum.Load(), client.calls.Load())
	}
}

func TestConfiguredQuoteWaveCannotWaitBeyondSourceAdmissionWindow(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 4)
	runtime.quoteTurns = newQuoteSendBudget(1)
	runtime.quoteBudget = newQuoteSendBudget(1)
	runtime.cfg.OperationTimeout = 2 * time.Second
	occupied, err := runtime.quoteBudget.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.release()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	work, releaseTurn, err := runtime.admitMakerTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseTurn()
	request, err := runtime.prepareDirectQuote(work, cycle, cycle.targets[0])
	if err != nil {
		t.Fatal(err)
	}
	cycle.authority.Book.ValidUntilMs = time.Now().Add(650 * time.Millisecond).UnixMilli()
	err = runtime.sendQuoteWave(work, cycle, []gateway.QuoteRequest{request})
	if !errors.Is(err, context.DeadlineExceeded) || client.calls.Load() != 0 {
		t.Fatalf("queued configured wave used expired source: err=%v sends=%d", err, client.calls.Load())
	}
}

func TestConfiguredPlaceSubBatchYieldsToQueuedFund(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	runtime.quoteBudget = newQuoteSendBudget(1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	done := make(chan error, 1)
	workers.Go(func() { done <- runtime.fillQuoteDepth(ctx, cycle) })
	select {
	case <-client.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	peerStarted, releasePeer := make(chan error, 1), make(chan struct{})
	workers.Go(func() {
		permit, err := runtime.quoteBudget.acquire(ctx)
		peerStarted <- err
		if err != nil {
			return
		}
		defer permit.release()
		select {
		case <-releasePeer:
		case <-ctx.Done():
		}
	})
	waitQuoteOwners(t, runtime.quoteBudget, 1)
	close(client.release)
	select {
	case err := <-peerStarted:
		if err != nil || client.calls.Load() != 1 {
			t.Fatalf("place batch delayed queued fund until %d SDK calls: %v", client.calls.Load(), err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(releasePeer)
	if err := <-done; err != nil || cycle.result.Placed != 8 || client.calls.Load() != 8 || client.maximum.Load() != 1 {
		t.Fatalf("yield changed SDK dispatch: placed=%d calls=%d concurrent=%d error=%v", cycle.result.Placed, client.calls.Load(), client.maximum.Load(), err)
	}
}

func TestFullDepthRefreshDoesNotQueueAheadOfRefill(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cycle.bridgeSlots = 1
	for index, target := range cycle.targets {
		cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
			OrderId:          strconv.Itoa(index + 1),
			ClientId:         native.InstanceClientPrefix(cycle.profile) + strconv.Itoa(index),
			Side:             target.Side,
			Price:            target.Price,
			Quantity:         target.Quantity,
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
	}
	runtime.quoteSlots = make(chan struct{}, 1)
	runtime.quoteSlots <- struct{}{}
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	select {
	case err := <-done:
		if err != nil || client.calls.Load() != 0 || cycle.result.Placed != 0 || !cycle.retained.pending || len(runtime.quoteSlots) != 1 {
			t.Fatalf("optional refresh err=%v sends=%d placed=%d pending=%t occupied=%d", err, client.calls.Load(), cycle.result.Placed, cycle.retained.pending, len(runtime.quoteSlots))
		}
	case <-time.After(500 * time.Millisecond):
		cancel()
		err := <-done
		t.Fatalf("full-depth refresh queued behind transport congestion: %v", err)
	}
}

func batchQuoteSetup(ctx context.Context, t *testing.T, count int) (*Runtime, *quoteCycle, *blockedQuoteClient, *currentStateTest) {
	t.Helper()
	// Database construction is fixture work, not part of the SDK wave deadline.
	setupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(setupCtx, t)
	runtime := setup.runtime
	runtime.quoteSlots = make(chan struct{}, maximumConcurrentQuoteSends)
	runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	runtime.store.matching = true
	setup.profile.StrategyInstanceId = setup.instance.Id
	setup.profile.MaximumOrderNotional = "100"
	client := &blockedQuoteClient{
		started: make(chan gateway.QuoteRequest, 16),
		release: make(chan struct{}),
	}
	runtime.client.Store(&gatewaySession{Client: client})
	denyMakerOrderStorage(t, setup.db)
	cycle := &quoteCycle{
		profile:  setup.profile,
		snapshot: setup.snapshot,
		retained: &quoteRetention{targets: make(map[string]bool)},
		result:   &execution.QuoteResult{},
		authority: LiveAuthority{
			InstanceID: setup.instance.Id,
			Revision:   setup.instance.Revision,
			Book: &common.VenueBook{
				ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
				Bids:         []*common.VenueLevel{{Price: "99"}},
				Asks:         []*common.VenueLevel{{Price: "101"}},
			},
		},
	}
	for level := range count {
		price := decimal.NewFromInt(99).Sub(decimal.NewFromInt(int64(level)).Shift(-1))
		side := domain.SideBuy
		if level%2 != 0 {
			price, side = decimal.NewFromInt(101).Add(decimal.NewFromInt(int64(level)).Shift(-1)), domain.SideSell
		}
		cycle.targets = append(cycle.targets, &common.VenueTarget{
			Side:     string(side),
			Price:    price.String(),
			Quantity: "0.1",
		})
	}
	return runtime, cycle, client, setup
}
