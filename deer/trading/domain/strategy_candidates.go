package domain

import "context"

// StrategyCandidates discovers persisted instances in one explicit account,
// including stopped requests that still need runtime completion.
type StrategyCandidates interface {
	ForAccount(ctx context.Context, accountID string) ([]int64, error)
}
