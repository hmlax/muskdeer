package deer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Cleanup releases owned resources. It must honor the supplied shutdown context.
type Cleanup func(context.Context) error

// Initializer acquires one named dependency and returns its release operation.
// Open must also return cleanup when it acquired resources before returning an error.
type Initializer struct {
	Name string
	Open func(context.Context) (Cleanup, error)
}

func initializeResources(ctx context.Context, plan []Initializer, timeout time.Duration) (Cleanup, error) {
	if ctx == nil {
		return nil, errors.New("initialization context is required")
	}
	if timeout <= 0 {
		return nil, errors.New("cleanup timeout must be positive")
	}
	plan = slices.Clone(plan)
	names := make(map[string]struct{}, len(plan))
	for _, resource := range plan {
		if resource.Name == "" || resource.Open == nil {
			return nil, errors.New("each resource requires a name and initializer")
		}
		if _, exists := names[resource.Name]; exists {
			return nil, fmt.Errorf("duplicate resource name %q", resource.Name)
		}
		names[resource.Name] = struct{}{}
	}
	cleanups := make([]Cleanup, 0, len(plan))
	rollback := func(cause error) (Cleanup, error) {
		return nil, errors.Join(cause, resourceCleanup(cleanups, timeout)(ctx))
	}
	for _, resource := range plan {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		cleanup, err := resource.Open(ctx)
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
		if err != nil {
			return rollback(fmt.Errorf("initialize resource %q: %w", resource.Name, err))
		}
		if cleanup == nil {
			return rollback(fmt.Errorf("resource %q did not provide cleanup", resource.Name))
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	return resourceCleanup(cleanups, timeout), nil
}

func resourceCleanup(cleanups []Cleanup, timeout time.Duration) Cleanup {
	cleanups = slices.Clone(cleanups)
	var (
		once   sync.Once
		result error
	)
	return func(ctx context.Context) error {
		if ctx == nil {
			return errors.New("cleanup context is required")
		}
		once.Do(func() {
			// A canceled request must not prevent release; cleanup gets its own deadline.
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()
			for index := len(cleanups) - 1; index >= 0; index-- {
				result = errors.Join(result, cleanups[index](shutdown))
			}
		})
		return result
	}
}
