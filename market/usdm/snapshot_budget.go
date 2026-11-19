package usdm

import (
	"context"
	"sync/atomic"
	"time"
)

// The process shares eight snapshot requests per second across all configured
// symbols. Cancelled waiters do not reserve future IP-weight capacity.
type snapshotBudget struct {
	gate    chan struct{}
	next    time.Time
	blocked atomic.Int64
}

func newSnapshotBudget() *snapshotBudget {
	return &snapshotBudget{gate: make(chan struct{}, 1)}
}

func (b *snapshotBudget) Wait(ctx context.Context) error {
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		until := b.next
		if blocked := time.UnixMilli(b.blocked.Load()); blocked.After(until) {
			until = blocked
		}
		timer := time.NewTimer(max(0, time.Until(until)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if time.Now().UnixMilli() >= b.blocked.Load() {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.next = time.Now().Add(time.Second / 8)
	return nil
}

func (b *snapshotBudget) backoff(until time.Time) {
	for previous := b.blocked.Load(); until.UnixMilli() > previous; previous = b.blocked.Load() {
		if b.blocked.CompareAndSwap(previous, until.UnixMilli()) {
			return
		}
	}
}
