package business

import (
	"context"
	"time"
)

func runLoop(ctx context.Context, poll, timeout time.Duration, operation func(context.Context) (bool, error)) error {
	return runOwnedLoop(ctx, poll, func(work context.Context) (bool, error) {
		attempt, cancel := context.WithTimeout(work, timeout)
		defer cancel()
		return operation(attempt)
	})
}

// runOwnedLoop propagates the lifecycle context; operations own their native
// budgets. Batches must bound each constituent operation independently.
func runOwnedLoop(ctx context.Context, poll time.Duration, operation func(context.Context) (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := operation(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
