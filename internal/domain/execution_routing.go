package domain

import "context"

// ExecutionRouting observes an account's current execution instance. It grants
// no authority; the selected server must obtain and finally verify its lease.
type ExecutionRouting interface {
	ExecutionInstance(ctx context.Context, accountID string) (string, error)
}
