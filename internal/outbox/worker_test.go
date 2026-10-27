package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
)

type workerStore struct {
	claim     *Record
	complete  error
	published int
	retried   int
	failed    int
}

func (s *workerStore) Claim(context.Context, string, time.Duration) (*Record, error) {
	record := s.claim
	s.claim = nil
	return record, nil
}

func (s *workerStore) Complete(context.Context, *Record) error {
	s.published++
	return s.complete
}

func (s *workerStore) Retry(context.Context, *Record, string, time.Duration) error {
	s.retried++
	return nil
}

func (s *workerStore) Fail(context.Context, *Record, string) error {
	s.failed++
	return nil
}

type workerPublisher struct {
	count int
	err   error
}

func (p *workerPublisher) Publish(context.Context, *commonv1.ExecutionEvent) error {
	p.count++
	return p.err
}

func TestWorkerDoesNotRepublishAfterAmbiguousDatabaseCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "outbox-test")
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("completion not recorded")
	store := &workerStore{
		claim:    &Record{ID: 1},
		complete: injected,
	}
	publisher := &workerPublisher{}
	worker, err := New(store, publisher, owner)
	if err != nil {
		t.Fatal(err)
	}
	// The worker deliberately delegates envelope validation to its store/typed
	// producer boundary; this fixture tests the commit acknowledgement window.
	worked, err := worker.RunOne(ctx)
	if !worked || !errors.Is(err, injected) || publisher.count != 1 || store.published != 1 || store.retried != 0 {
		t.Fatalf("completion failure replayed: %v, %v, calls=%d", worked, err, publisher.count)
	}
}

func TestWorkerPublishFailureSchedulesOneRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "outbox-test")
	if err != nil {
		t.Fatal(err)
	}
	store := &workerStore{claim: &Record{ID: 1}}
	publisher := &workerPublisher{err: context.DeadlineExceeded}
	worker, err := New(store, publisher, owner)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOne(ctx); !worked || !errors.Is(err, context.DeadlineExceeded) || publisher.count != 1 || store.retried != 1 || store.published != 0 {
		t.Fatalf("publish uncertainty was not durably scheduled: %v, %v", worked, err)
	}
}

func TestWorkerQuarantinesMalformedEventsWithoutRetrying(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "outbox-test")
	if err != nil {
		t.Fatal(err)
	}
	store := &workerStore{claim: &Record{ID: 1}}
	publisher := &workerPublisher{err: ErrInvalidEvent}
	worker, err := New(store, publisher, owner)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOne(ctx); !worked || !errors.Is(err, ErrInvalidEvent) || publisher.count != 1 || store.failed != 1 || store.retried != 0 || store.published != 0 {
		t.Fatalf("malformed event was not quarantined: %v, %v", worked, err)
	}
}
