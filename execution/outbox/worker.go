// Package outbox coordinates one bounded publication attempt per durable claim.
// Broker acknowledgement and database completion are separate failure windows.
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	outboxcontract "github.com/hmlax/muskdeer/deer/trading/outboxcontract"
)

// Record retains shared claim identity across worker and storage.
type Record = outboxcontract.Record

// Store is the shared durable claim interface.
type Store = outboxcontract.Store

// Publisher confirms broker storage for a shared execution event.
type Publisher = outboxcontract.Publisher

// Stable publication codes and subject are shared with consumers and storage.
const (
	CodePublishFailed      = outboxcontract.CodePublishFailed
	CodeInvalidEvent       = outboxcontract.CodeInvalidEvent
	ExecutionSubject       = outboxcontract.ExecutionSubject
	messageOperationFailed = "outbox publication operation failed"
)

// Errors preserve shared claim and validation identity.
var (
	ErrClaimLost     = outboxcontract.ErrClaimLost
	ErrInvalidRecord = outboxcontract.ErrInvalidRecord
	ErrInvalidEvent  = outboxcontract.ErrInvalidEvent
)

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
