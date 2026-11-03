package domain

import "context"

// StrategyExecution borrows authenticated execution transport. Write routing is
// account-owned; observations contain no reusable owner authority or new key.
type StrategyExecution interface {
	Submit(ctx context.Context, input OrderInput) (IntentSnapshot, error)
	Cancel(ctx context.Context, input CancelInput) (CancellationResult, error)
	FindOrder(ctx context.Context, accountID, idempotencyKey string) (IntentSnapshot, error)
	Order(ctx context.Context, accountID, clientOrderID string) (IntentSnapshot, error)
	FindCancellation(ctx context.Context, accountID, idempotencyKey string) (CancellationResult, error)
	Orders(ctx context.Context, filter ExecutionFilter) ([]IntentSnapshot, int64, error)
	Account(ctx context.Context, accountID string) (ExecutionAccount, error)
}
