package domain

import (
	"context"
	"time"
)

// SimulatorScenarios observes explicit L1 scenario work without changing money.
// The simulator's privileged command must still confirm the actual terminal state.
type SimulatorScenarios interface {
	DueCancellation(ctx context.Context, accountID string, delay time.Duration) (string, error)
}
