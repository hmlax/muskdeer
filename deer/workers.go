package deer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v2/transport"
)

// Worker is a process-owned blocking task. Run must honor cancellation and
// return after joining any child work; it cannot retain a constructor context.
// Check is optional, read-only and must honor its readiness context.
type Worker struct {
	Name  string
	Run   func(context.Context) error
	Check func(context.Context) error
}

type managedWorker struct {
	spec      Worker
	onFailure func()
	ready     chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	cancel    context.CancelFunc
	running   bool
	finished  bool
	stopping  bool
	result    error
	stopOnce  sync.Once
	stopErr   error
}

type workerFailure struct {
	name  string
	cause error
}

var _ transport.Server = (*managedWorker)(nil)

func (e *workerFailure) Error() string { return "worker " + e.name + " failed" }
func (e *workerFailure) Unwrap() error { return e.cause }

func (d *Deer) addWorkers(ctx context.Context, configure func(context.Context, *Deer) ([]Worker, error)) error {
	if configure == nil {
		return nil
	}
	plan, err := configure(ctx, d)
	if err != nil {
		return fmt.Errorf("configure workers: %w", err)
	}
	if len(plan) > 32 {
		return errors.New("service allows at most 32 workers")
	}
	names := make(map[string]struct{}, len(plan))
	for _, worker := range plan {
		if !validWorkerName(worker.Name) || worker.Run == nil {
			return errors.New("worker requires a bounded safe name and blocking run function")
		}
		if _, exists := names[worker.Name]; exists {
			return errors.New("worker names must be unique")
		}
		names[worker.Name] = struct{}{}
	}
	for _, worker := range plan {
		d.workers = append(d.workers, &managedWorker{
			spec:      worker,
			onFailure: d.requestStop,
			ready:     make(chan struct{}),
			done:      make(chan struct{}),
		})
	}
	return nil
}

func (w *managedWorker) Start(ctx context.Context) error {
	work, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		cancel()
		return nil
	}
	if w.running {
		w.mu.Unlock()
		cancel()
		return errors.New("worker cannot start more than once")
	}
	w.running = true
	w.cancel = cancel
	close(w.ready)
	w.mu.Unlock()
	err := w.spec.Run(work)
	cancel()
	w.mu.Lock()
	if w.stopping && (err == nil || errors.Is(err, context.Canceled)) {
		err = nil
	}
	if err == nil && !w.stopping {
		err = errors.New("worker returned before shutdown")
	}
	if err != nil {
		err = &workerFailure{
			name:  w.spec.Name,
			cause: err,
		}
	}
	w.result = err
	w.finished = true
	w.mu.Unlock()
	if err != nil {
		w.onFailure()
	}
	close(w.done)
	return err
}

func (w *managedWorker) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.stopping = true
		if w.running {
			w.cancel()
		} else {
			close(w.done)
		}
		w.mu.Unlock()
		select {
		case <-w.done:
		case <-ctx.Done():
			w.stopErr = ctx.Err()
			// A callback violating cancellation cannot justify releasing its DB
			// or connection while it still runs. The process owns this join.
			<-w.done
		}
	})
	return w.stopErr
}

func (w *managedWorker) waitReady(ctx context.Context) error {
	select {
	case <-w.ready:
		return w.check(ctx)
	case <-w.done:
		return errors.New("worker stopped before readiness")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *managedWorker) check(ctx context.Context) error {
	w.mu.Lock()
	active, result := w.running && !w.finished && !w.stopping, w.result
	w.mu.Unlock()
	if !active {
		return errors.Join(ErrNotReady, result)
	}
	if w.spec.Check != nil {
		return w.spec.Check(ctx)
	}
	return ctx.Err()
}

func (d *Deer) stopOwned(ctx context.Context) error {
	var result error
	for _, worker := range d.workers {
		result = errors.Join(result, worker.Stop(ctx))
	}
	for _, server := range d.servers {
		result = errors.Join(result, server.Stop(ctx))
	}
	return result
}

func validWorkerName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index := range len(name) {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
