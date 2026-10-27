package domain

import (
	"context"
	"time"
)

// MaximumExecutionPage bounds both query work and response size.
const (
	MaximumExecutionPage   = 100
	MaximumExecutionAssets = 128
)

// ExecutionFilter uses keyset pagination inside one explicit account scope.
type ExecutionFilter struct {
	AccountID    string
	StrategyID   int64
	InstrumentID int64
	AfterID      int64
	Limit        int
	OpenOnly     bool
}

// ExecutionFill exposes authoritative execution facts, including direct L1
// simulator scenarios that have no OMS intent.
type ExecutionFill struct {
	Fill          CoreFill
	AccountID     string
	ClientOrderID string
	StrategyID    int64
	InstrumentID  int64
	Side          Side
	OccurredAt    time.Time
}

// ExecutionAccount is one consistent account revision and its asset balances.
type ExecutionAccount struct {
	AccountID  string
	Sequence   int64
	ObservedAt time.Time
	Balances   []CoreBalance
}

// ExecutionQueries never submits or changes funds. Implementations enforce
// deadlines, account scope and the maximum page size at the repository edge.
type ExecutionQueries interface {
	Orders(ctx context.Context, filter ExecutionFilter) ([]IntentSnapshot, int64, error)
	Fills(ctx context.Context, filter ExecutionFilter) ([]ExecutionFill, int64, error)
	Account(ctx context.Context, accountID string) (ExecutionAccount, error)
}
