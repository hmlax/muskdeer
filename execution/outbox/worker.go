// Package outbox coordinates one bounded publication attempt per durable claim.
// Broker acknowledgement and database completion are separate failure windows.
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
)

// Stable operation codes and transport subject suffix.
const (
	CodePublishFailed         = "PUBLISH_FAILED"
	CodeInvalidEvent          = "INVALID_EVENT"
	ExecutionSubject          = "execution.order.v1"
	MessagePersistenceFailure = "outbox persistence operation failed"
	messageOperationFailed    = "outbox publication operation failed"
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

// Worker borrows its dependencies and starts no background task.
type Worker struct {
	store     Store
	publisher Publisher
	owner     string
}

type operationError struct{ cause error }

func (*operationError) Error() string   { return messageOperationFailed }
func (e *operationError) Unwrap() error { return e.cause }

// New binds one process incarnation to durable claims, not to a Nacos name.
func New(store Store, publisher Publisher, owner string) (*Worker, error) {
	if store == nil || publisher == nil || !ownerid.Valid(owner) {
		return nil, ErrInvalidRecord
	}
	return &Worker{
		store:     store,
		publisher: publisher,
		owner:     owner,
	}, nil
}

// RunOne processes at most one event. A completion error leaves the claim for
// recovery; it cannot justify a second publication inside the same call.
func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, ErrInvalidRecord
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return false, ErrInvalidRecord
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	record, err := w.store.Claim(ctx, w.owner, 30*time.Second)
	if err != nil || record == nil {
		return false, err
	}
	if err := w.publisher.Publish(ctx, record.Event); err != nil {
		var saveErr error
		if errors.Is(err, ErrInvalidEvent) {
			saveErr = w.store.Fail(ctx, record, CodeInvalidEvent)
		} else {
			saveErr = w.store.Retry(ctx, record, CodePublishFailed, time.Second)
		}
		return true, &operationError{cause: errors.Join(err, saveErr)}
	}
	if err := w.store.Complete(ctx, record); err != nil {
		return true, &operationError{cause: err}
	}
	return true, nil
}
