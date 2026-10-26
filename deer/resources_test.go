package deer

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestResourcesRollbackKeepsOriginalError(t *testing.T) {
	t.Parallel()
	failure := errors.New("second dependency unavailable")
	closeFailure := errors.New("first dependency close failed")
	var closed atomic.Bool
	_, err := initializeResources(t.Context(), []Initializer{
		{
			Name: "first",
			Open: func(context.Context) (Cleanup, error) {
				return func(context.Context) error {
					closed.Store(true)
					return closeFailure
				}, nil
			},
		},
		{
			Name: "second",
			Open: func(context.Context) (Cleanup, error) { return nil, failure },
		},
	}, time.Second)
	if !closed.Load() || !errors.Is(err, failure) || !errors.Is(err, closeFailure) {
		t.Errorf("initializeResources(failure) = closed %v, error %v; want both errors and cleanup", closed.Load(), err)
	}
}

func TestResourcesCleanupReverseOrderAndIdempotence(t *testing.T) {
	t.Parallel()
	var sequence []string
	makeInitializer := func(name string) Initializer {
		return Initializer{
			Name: name,
			Open: func(context.Context) (Cleanup, error) {
				return func(context.Context) error {
					sequence = append(sequence, name)
					return nil
				}, nil
			},
		}
	}
	cleanup, err := initializeResources(t.Context(), []Initializer{
		makeInitializer("first"),
		makeInitializer("second"),
		makeInitializer("third"),
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := cleanup(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(sequence, []string{"third", "second", "first"}) {
		t.Errorf("cleanup sequence = %v, want third/second/first exactly once", sequence)
	}
}

func TestResourcesCanceledConstructionStillRollsBack(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var closed atomic.Bool
	_, err := initializeResources(ctx, []Initializer{
		{
			Name: "canceling dependency",
			Open: func(context.Context) (Cleanup, error) {
				cancel()
				return func(ctx context.Context) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					closed.Store(true)
					return nil
				}, nil
			},
		},
	}, time.Second)
	if !errors.Is(err, context.Canceled) || !closed.Load() {
		t.Errorf("canceled initialization = %v, closed %v; want Canceled and independent rollback context", err, closed.Load())
	}
}

func TestResourcesConcurrentCleanupReleasesOnce(t *testing.T) {
	t.Parallel()
	var count atomic.Int64
	cleanup, err := initializeResources(t.Context(), []Initializer{
		{
			Name: "shared dependency",
			Open: func(context.Context) (Cleanup, error) {
				return func(context.Context) error {
					count.Add(1)
					return nil
				}, nil
			},
		},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			if err := cleanup(t.Context()); err != nil {
				t.Errorf("concurrent cleanup = %v", err)
			}
		})
	}
	callers.Wait()
	if count.Load() != 1 {
		t.Errorf("concurrent cleanup count = %d, want 1", count.Load())
	}
}

func TestResourcesValidateBeforeOpeningDependencies(t *testing.T) {
	t.Parallel()
	var opened atomic.Bool
	_, err := initializeResources(t.Context(), []Initializer{
		{
			Name: "valid",
			Open: func(context.Context) (Cleanup, error) {
				opened.Store(true)
				return func(context.Context) error { return nil }, nil
			},
		},
		{Name: "missing factory"},
	}, time.Second)
	if err == nil || opened.Load() {
		t.Errorf("invalid resource plan = %v, opened %v; want rejection before side effects", err, opened.Load())
	}
}

func TestResourcesPartiallyOpenedFailureIsAlsoReleased(t *testing.T) {
	t.Parallel()
	failure := errors.New("partially opened dependency failed")
	var count atomic.Int64
	_, err := initializeResources(t.Context(), []Initializer{
		{
			Name: "partial",
			Open: func(context.Context) (Cleanup, error) {
				return func(context.Context) error {
					count.Add(1)
					return nil
				}, failure
			},
		},
	}, time.Second)
	if !errors.Is(err, failure) || count.Load() != 1 {
		t.Errorf("partial initialization = %v, cleanup count %d; want original failure and one release", err, count.Load())
	}
}

func TestResourcesCleanupKeepsErrorsAndDeadline(t *testing.T) {
	t.Parallel()
	failure := errors.New("dependency shutdown error")
	cleanup, err := initializeResources(t.Context(), []Initializer{
		{
			Name: "bounded",
			Open: func(context.Context) (Cleanup, error) {
				return func(ctx context.Context) error {
					deadline, exists := ctx.Deadline()
					if !exists || time.Until(deadline) > time.Second {
						t.Error("cleanup context must have the configured bounded deadline")
					}
					return failure
				}, nil
			},
		},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := cleanup(t.Context()); !errors.Is(err, failure) {
			t.Errorf("cleanup error = %v, want original dependency shutdown error", err)
		}
	}
}
