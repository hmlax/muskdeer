package deer

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkersJoinBeforeDependenciesClose(t *testing.T) {
	var stopped, released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("worker-lifetime"), ServiceSpec{
		LogOutput: io.Discard,
		Resources: []Initializer{
			{
				Name: "worker-dependency",
				Open: func(context.Context) (Cleanup, error) {
					return func(context.Context) error {
						if stopped.Load() != 2 {
							return errors.New("dependency closed before its workers joined")
						}
						released.Add(1)
						return nil
					}, nil
				},
			},
		},
		Workers: func(context.Context, *Deer) ([]Worker, error) {
			run := func(ctx context.Context) error {
				<-ctx.Done()
				stopped.Add(1)
				return ctx.Err()
			}
			return []Worker{
				{
					Name: "publisher",
					Run:  run,
				},
				{
					Name: "consumer",
					Run:  run,
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 2 || released.Load() != 1 {
		t.Fatalf("worker/resource lifecycle = stopped=%d released=%d", stopped.Load(), released.Load())
	}
}

func TestWorkerFailureStopsSiblingAndUnpublishesReadiness(t *testing.T) {
	failure := errors.New("controlled worker failure")
	gate := make(chan struct{})
	siblingDone := make(chan struct{})
	service, _, err := NewDeer(t.Context(), serviceConfig("worker-failure"), ServiceSpec{
		LogOutput: io.Discard,
		Workers: func(context.Context, *Deer) ([]Worker, error) {
			return []Worker{
				{
					Name: "failed",
					Run: func(ctx context.Context) error {
						select {
						case <-gate:
							return failure
						case <-ctx.Done():
							return ctx.Err()
						}
					},
				},
				{
					Name: "sibling",
					Run: func(ctx context.Context) error {
						defer close(siblingDone)
						<-ctx.Done()
						return ctx.Err()
					},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	close(gate)
	select {
	case err := <-result:
		if !errors.Is(err, failure) || service.Ready() {
			t.Fatalf("worker failure was hidden: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker failure did not stop its service")
	}
	select {
	case <-siblingDone:
	default:
		t.Fatal("service returned before joining its sibling worker")
	}
}

func TestUnusedAndInvalidWorkersNeverStartAndRollBackResources(t *testing.T) {
	var started, released atomic.Int64
	spec := ServiceSpec{
		LogOutput: io.Discard,
		Resources: []Initializer{countedResource(&released)},
		Workers: func(context.Context, *Deer) ([]Worker, error) {
			return []Worker{
				{
					Name: "valid",
					Run: func(context.Context) error {
						started.Add(1)
						return nil
					},
				},
			}, nil
		},
	}
	_, cleanup, err := NewDeer(t.Context(), serviceConfig("unused-worker"), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(t.Context()); err != nil || started.Load() != 0 || released.Load() != 1 {
		t.Fatalf("unused workers started or leaked: %v", err)
	}
	spec.Workers = func(context.Context, *Deer) ([]Worker, error) {
		return []Worker{
			{
				Name: "duplicate",
				Run:  func(context.Context) error { return nil },
			},
			{
				Name: "duplicate",
				Run:  func(context.Context) error { return nil },
			},
		}, nil
	}
	if service, cleanup, err := NewDeer(t.Context(), serviceConfig("invalid-workers"), spec); err == nil || service != nil || cleanup != nil || released.Load() != 2 {
		t.Fatalf("invalid plan did not roll back: %v", err)
	}
}

func TestWorkerStartupCheckFailureJoinsRunningTask(t *testing.T) {
	failure := errors.New("controlled worker readiness failure")
	entered := make(chan struct{})
	exited := make(chan struct{})
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("worker-startup-check"), ServiceSpec{
		LogOutput: io.Discard,
		Workers: func(context.Context, *Deer) ([]Worker, error) {
			return []Worker{
				{
					Name: "unready",
					Run: func(ctx context.Context) error {
						defer close(exited)
						close(entered)
						<-ctx.Done()
						return ctx.Err()
					},
					Check: func(ctx context.Context) error {
						select {
						case <-entered:
							return failure
						case <-ctx.Done():
							return ctx.Err()
						}
					},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.Run(ctx); !errors.Is(err, failure) || service.Ready() {
		t.Fatalf("startup check failure was ignored: %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("startup failure released a still-running task")
	}
}
