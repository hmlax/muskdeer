// Package outboxcontract defines durable publication claims shared by persistence and execution.
package outboxcontract

import (
	"context"
	"errors"
	"time"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
)

// Stable operation codes and transport subject suffix.
const (
	CodePublishFailed         = "PUBLISH_FAILED"
	CodeInvalidEvent          = "INVALID_EVENT"
	ExecutionSubject          = "execution.order.v1"
	MessagePersistenceFailure = "outbox persistence operation failed"
	messageClaimLost          = "outbox claim is no longer current"
	messageInvalidRecord      = "invalid outbox record or call"
)

// Errors preserve typed failure classification without database/broker text.
var (
	ErrClaimLost     = errors.New(messageClaimLost)
	ErrInvalidRecord = errors.New(messageInvalidRecord)
	ErrInvalidEvent  = errors.New(CodeInvalidEvent)
)

// Record carries a proposed row claim; expiry and owner are verified by Store.
type Record struct {
	ID        int64
	Owner     string
	ExpiresAt int64
	Event     *commonv1.ExecutionEvent
}

// Store uses database time and row claims. Complete must reject stale owners.
type Store interface {
	Claim(ctx context.Context, owner string, ttl time.Duration) (*Record, error)
	Complete(ctx context.Context, record *Record) error
	Retry(ctx context.Context, record *Record, code string, delay time.Duration) error
	Fail(ctx context.Context, record *Record, code string) error
}

// Publisher confirms storage once and never retries internally.
type Publisher interface {
	Publish(ctx context.Context, event *commonv1.ExecutionEvent) error
}
