package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
)

func TestQuoteMailboxCoalescesCopiesAndRejectsReversedObservations(t *testing.T) {
	t.Parallel()
	mailbox := newQuoteMailbox()
	for sequence := int64(1); sequence <= 100; sequence++ {
		plan := mailboxTestPlan(sequence)
		mailbox.offer(plan)
		plan.Targets[0].Price = "changed after admission"
	}
	mailbox.offer(mailboxTestPlan(1))
	got := mailbox.pending.Load()
	if got.Book.Sequence != 100 || got.Targets[0].Price != "99" || len(mailbox.wake) != 1 {
		t.Fatal("mailbox did not preserve one immutable latest target")
	}
	newer := mailboxTestPlan(100)
	newer.Book.SnapshotStartedAtMs = got.Book.EventAtMs + 1
	mailbox.offer(newer)
	mailbox.offer(got)
	if mailbox.pending.Load().Book.SnapshotStartedAtMs != newer.Book.SnapshotStartedAtMs {
		t.Fatal("older observation replaced a newer same-sequence snapshot")
	}
}

func TestQuoteMailboxJoinsWriterAndSkipsIntermediateTargets(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mailbox := newQuoteMailbox()
	started := make(chan int64, 2)
	release := make(chan struct{})
	joined := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- mailbox.run(ctx, 5*time.Second, func(work context.Context, plan *pb.QuotePlan) error {
			started <- plan.GetBook().GetSequence()
			if plan.GetBook().GetSequence() == 1 {
				select {
				case <-release:
				case <-work.Done():
					return work.Err()
				}
				return nil
			}
			<-work.Done()
			<-joined
			return work.Err()
		})
	}()
	mailbox.offer(mailboxTestPlan(1))
	if got := <-started; got != 1 {
		t.Fatalf("first target = %d", got)
	}
	for sequence := int64(2); sequence <= 100; sequence++ {
		mailbox.offer(mailboxTestPlan(sequence))
	}
	close(release)
	if got := <-started; got != 100 {
		t.Fatalf("applied intermediate target %d instead of the latest", got)
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("mailbox released ownership before its writer joined: %v", err)
	default:
	}
	close(joined)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("stop = %v", err)
	}
}

func TestQuoteMailboxExpiredTargetOnlyRunsProtection(t *testing.T) {
	t.Parallel()
	mailbox := newQuoteMailbox()
	plan := mailboxTestPlan(1)
	plan.Book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
	mailbox.offer(plan)
	want := errors.New("protection observed")
	err := mailbox.run(t.Context(), time.Second, func(ctx context.Context, got *pb.QuotePlan) error {
		if got != nil {
			t.Fatal("expired target was admitted to the writer")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("worker operation has no bound")
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("protection = %v", err)
	}
}

func mailboxTestPlan(sequence int64) *pb.QuotePlan {
	return &pb.QuotePlan{
		Book: &common.VenueBook{
			Sequence:     sequence,
			EventAtMs:    time.Now().UnixMilli(),
			ValidUntilMs: time.Now().Add(5 * time.Second).UnixMilli(),
		},
		Targets: []*common.VenueTarget{{Price: "99"}},
	}
}

func TestMakerHandoffRequiresANewerUnexpiredAcceptedObservation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"new_sequence", "new_snapshot", "same_observation", "reversed", "expired"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			runtime := &Runtime{mailbox: newQuoteMailbox()}
			previous := mailboxTestPlan(10)
			next := mailboxTestPlan(11)
			want := true
			switch kind {
			case "new_snapshot":
				next.Book.Sequence = previous.Book.Sequence
				next.Book.SnapshotStartedAtMs = previous.Book.EventAtMs + 1
			case "same_observation":
				next.Book.Sequence, next.Book.EventAtMs = previous.Book.Sequence, previous.Book.EventAtMs
				want = false
			case "reversed":
				next.Book.Sequence = 9
				want = false
			case "expired":
				next.Book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
				want = false
			}
			runtime.mailbox.offer(next)
			if got := runtime.newerMakerIntent(previous); (got != nil) != want {
				t.Fatalf("new target handoff(%s) = %v, want eligible=%t", kind, got, want)
			}
		})
	}
}
