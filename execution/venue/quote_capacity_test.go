package venue

import (
	"context"
	"testing"
	"time"
)

func TestQuoteSendCapacityAllows64AndQueuesNextOwner(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	budget := newQuoteSendBudget(maximumConcurrentQuoteSends)
	permits := make([]*quoteSendPermit, 0, 64)
	defer func() {
		for _, permit := range permits {
			permit.release()
		}
	}()
	for index := range 64 {
		permit, err := budget.acquire(ctx)
		if err != nil {
			t.Fatalf("acquire owner %d = %v, want 64 simultaneous owners", index+1, err)
		}
		permits = append(permits, permit)
	}

	type acquisition struct {
		permit *quoteSendPermit
		err    error
	}
	joined := make(chan acquisition, 1)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		permit, err := budget.acquire(ctx)
		joined <- acquisition{
			permit: permit,
			err:    err,
		}
	}()
	defer func() {
		cancel()
		<-workerDone
		select {
		case result := <-joined:
			if result.permit != nil {
				result.permit.release()
			}
		default:
		}
	}()
	waitQuoteOwners(t, budget, 1)
	permits[0].release()
	result := <-joined
	if result.err != nil {
		t.Fatalf("acquire queued owner = %v, want released capacity", result.err)
	}
	result.permit.release()
	for _, permit := range permits {
		permit.release()
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.active != 0 || len(budget.waiting) != 0 {
		t.Fatalf("released capacity = active %d, waiting %d, want 0, 0", budget.active, len(budget.waiting))
	}
}
