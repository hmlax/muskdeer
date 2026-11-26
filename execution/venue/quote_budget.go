package venue

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Each existing maker lifecycle can queue one declarative turn. Capacity is
// granted FIFO before loading observations or starting the operation deadline.
// A turn reserves one SDK send; spare capacity can widen its joined waves only
// when no other owner is waiting. The process send bound remains unchanged.
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

func newQuoteSendBudget(limit int) *quoteSendBudget {
	return &quoteSendBudget{limit: limit}
}

func (b *quoteSendBudget) acquire(ctx context.Context) (*quoteSendPermit, error) {
	started := time.Now()
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
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
	if err := ctx.Err(); err != nil {
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
		if waiter.ctx.Err() == nil {
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
