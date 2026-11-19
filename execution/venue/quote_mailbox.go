package venue

import (
	"context"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
)

// Targets are declarative: keep one immutable latest intent while the fund's
// existing writer finishes. No SDK request or acknowledgement is queued here.
type quoteMailbox struct {
	pending atomic.Pointer[pb.QuotePlan]
	wake    chan struct{}
}

func newQuoteMailbox() *quoteMailbox {
	return &quoteMailbox{wake: make(chan struct{}, 1)}
}

func (m *quoteMailbox) offer(plan *pb.QuotePlan) {
	next := proto.CloneOf(plan)
	for {
		previous := m.pending.Load()
		if previous != nil && (previous.Book.Sequence > next.Book.Sequence || previous.Book.Sequence == next.Book.Sequence && quoteObservedAt(previous) > quoteObservedAt(next)) {
			return
		}
		if m.pending.CompareAndSwap(previous, next) {
			break
		}
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func quoteObservedAt(plan *pb.QuotePlan) int64 {
	if observed := plan.GetBook().GetSnapshotStartedAtMs(); observed > 0 {
		return observed
	}
	return plan.GetBook().GetEventAtMs()
}

// The existing lifecycle goroutine calls step synchronously. Cancellation joins
// its in-flight SDK work; a wake never spawns a second financial writer.
func (m *quoteMailbox) run(ctx context.Context, timeout time.Duration, step func(context.Context, *pb.QuotePlan) error) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-m.wake:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		plan := m.pending.Load()
		if plan.GetBook().GetValidUntilMs() <= time.Now().UnixMilli() {
			plan = nil
		}
		work, cancel := context.WithTimeout(ctx, timeout)
		err := step(work, plan)
		cancel()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
