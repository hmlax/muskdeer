package venue

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// The observation turns and SDK waves use separate FIFO budgets. A turn waits
// before observing, but never holds send capacity while reading. An SDK wave
// reserves one send and borrows spare capacity only without waiting peers.
type quoteSendBudget struct {
	mu      sync.Mutex
	limit   int
	active  int
	waiting []*quoteSendWaiter
}

type quoteSendWaiter struct {
	ctx     context.Context
	ready   chan struct{}
	granted bool // guarded by quoteSendBudget.mu
}

type quoteSendPermit struct {
	budget   *quoteSendBudget
	waited   time.Duration
	released atomic.Bool
}

type quoteSendPermitKey struct{}
type quoteTurnPermitKey struct{}

func quoteBudgetContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func newQuoteSendBudget(limit int) *quoteSendBudget {
	return &quoteSendBudget{limit: limit}
}

func (b *quoteSendBudget) acquire(ctx context.Context) (*quoteSendPermit, error) {
	started := time.Now()
	b.mu.Lock()
	if err := quoteBudgetContextError(ctx); err != nil {
		b.mu.Unlock()
		return nil, err
	}
	if b.active < b.limit && len(b.waiting) == 0 {
		b.active++
		b.mu.Unlock()
		return &quoteSendPermit{budget: b}, nil
	}
	waiter := &quoteSendWaiter{
		ctx:   ctx,
		ready: make(chan struct{}),
	}
	b.waiting = append(b.waiting, waiter)
	b.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-waiter.ready:
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := quoteBudgetContextError(ctx); err != nil {
		if waiter.granted {
			b.give(1)
		} else {
			for i, candidate := range b.waiting {
				if candidate == waiter {
					copy(b.waiting[i:], b.waiting[i+1:])
					b.waiting[len(b.waiting)-1] = nil
					b.waiting = b.waiting[:len(b.waiting)-1]
					break
				}
			}
		}
		return nil, err
	}
	return &quoteSendPermit{
		budget: b,
		waited: time.Since(started),
	}, nil
}

// give is called with mu held. Only this owner closes waiter.ready, exactly once.
func (b *quoteSendBudget) give(count int) {
	b.active -= count
	for b.active < b.limit && len(b.waiting) > 0 {
		waiter := b.waiting[0]
		b.waiting[0] = nil
		b.waiting = b.waiting[1:]
		if quoteBudgetContextError(waiter.ctx) == nil {
			waiter.granted = true
			b.active++
		}
		close(waiter.ready)
	}
}

func (p *quoteSendPermit) release() {
	if !p.released.CompareAndSwap(false, true) {
		return
	}
	p.budget.mu.Lock()
	p.budget.give(1)
	p.budget.mu.Unlock()
}

func (p *quoteSendPermit) borrow(wanted int) int {
	b := p.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if p.released.Load() || len(b.waiting) > 0 {
		return 0
	}
	count := min(wanted, b.limit-b.active)
	b.active += count
	return count
}

func withQuoteSendPermit(ctx context.Context, permit *quoteSendPermit) context.Context {
	return context.WithValue(ctx, quoteSendPermitKey{}, permit)
}

func quoteSendPermitFrom(ctx context.Context) *quoteSendPermit {
	permit, _ := ctx.Value(quoteSendPermitKey{}).(*quoteSendPermit)
	return permit
}

func quoteTurnPermitFrom(ctx context.Context) *quoteSendPermit {
	permit, _ := ctx.Value(quoteTurnPermitKey{}).(*quoteSendPermit)
	return permit
}

func quoteWaveSize(ctx context.Context, wanted int) (int, func()) {
	permit := quoteSendPermitFrom(ctx)
	if permit == nil || wanted == 0 {
		return wanted, func() {}
	}
	extra := permit.borrow(wanted - 1)
	var once sync.Once
	return 1 + extra, func() {
		once.Do(func() {
			permit.budget.mu.Lock()
			permit.budget.give(extra)
			permit.budget.mu.Unlock()
		})
	}
}

func (r *Runtime) reserveQuoteSend(ctx context.Context) (context.Context, func(), error) {
	if r.quoteBudget == nil {
		return ctx, func() {}, nil
	}
	if permit := quoteSendPermitFrom(ctx); permit != nil {
		if permit.budget != r.quoteBudget || permit.released.Load() {
			return ctx, nil, ErrInvalidConfig
		}
		return ctx, func() {}, nil
	}
	permit, err := r.quoteBudget.acquire(ctx)
	if err != nil {
		return ctx, nil, err
	}
	return withQuoteSendPermit(ctx, permit), permit.release, nil
}

func (r *Runtime) reserveQuoteWave(ctx context.Context, wanted int, operation string, until time.Time) (context.Context, int, func(), error) {
	if r.quoteBudget == nil || quoteSendPermitFrom(ctx) != nil || wanted == 0 {
		width, release := quoteWaveSize(ctx, wanted)
		return ctx, width, release, nil
	}
	queue := ctx
	if !until.IsZero() {
		var cancel context.CancelFunc
		queue, cancel = context.WithDeadline(ctx, until)
		defer cancel()
	}
	finish := r.traceQuoteAdmissionStage(queue, "execution.quote.wave.admission", operation)
	reserved, release, err := r.reserveQuoteSend(queue)
	finish(err)
	if err != nil {
		return ctx, 0, nil, err
	}
	// Acquisition used its shorter queue deadline. The SDK call keeps the
	// original parent and rechecks its unchanged source/account send window.
	work := withQuoteSendPermit(ctx, quoteSendPermitFrom(reserved))
	width, releaseExtra := quoteWaveSize(work, wanted)
	return work, width, func() {
		releaseExtra()
		release()
	}, nil
}
