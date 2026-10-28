package domain

import "context"

// MaxAccountStrategyInstances bounds a serial account cycle and its local plan.
const MaxAccountStrategyInstances = 16

// StrategyCandidates discovers persisted instances in one explicit account,
// including stopped requests that still need runtime completion.
type StrategyCandidates interface {
	ForAccount(ctx context.Context, accountID string) ([]int64, error)
}
