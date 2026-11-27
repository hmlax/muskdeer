package venue

import (
	"context"
	"errors"
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
	next.AcceptedAtUnixNano = time.Now().UnixNano()
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
	return m.runAdmitted(ctx, timeout, nil, step)
}

func (m *quoteMailbox) runAdmitted(ctx context.Context, timeout time.Duration, admit func(context.Context) (context.Context, func(), error), step func(context.Context, *pb.QuotePlan) error) error {
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
		owner, release := ctx, func() {}
		if admit != nil {
			var err error
			owner, release, err = admit(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if errors.Is(err, context.DeadlineExceeded) {
					continue // No observation, SDK payload or identity was queued.
				}
				return err
			}
		}
		plan := m.pending.Load()
		if plan.GetBook().GetValidUntilMs() <= time.Now().UnixMilli() {
			plan = nil
		}
		err := func() error {
			defer release()
			work, cancel := context.WithTimeout(owner, timeout)
			defer cancel()
			return step(work, plan)
		}()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
