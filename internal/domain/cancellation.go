package domain

import "context"

// CancellationState belongs to the durable command, independently of the
// order's last confirmed core cancellation state.
type CancellationState string

// Durable control states and command namespaces.
const (
	CancellationRequested        CancellationState = "REQUESTED"
	CancellationSubmitting       CancellationState = "SUBMITTING"
	CancellationPending          CancellationState = "PENDING"
	CancellationConfirmed        CancellationState = "CONFIRMED"
	CancellationUnknown          CancellationState = "UNKNOWN"
	CancellationRejected         CancellationState = "REJECTED"
	EventOrderCancelRequested                      = "OrderCancelRequested"
	EventOrderCancelStateChanged                   = "OrderCancelStateChanged"
	CoreSubmitKeyPrefix                            = "submit:"
	CoreCancelKeyPrefix                            = "cancel:"
)

// CancellationSnapshot retains a stable core command and proposed authority.
type CancellationSnapshot struct {
	ID                 int64
	IntentID           int64
	Input              CancelInput
	ExecutionAuthority LeaseToken
	CoreCommandKey     string
	State              CancellationState
	Revision           int64
	ErrorCode          string
}

// CancellationResult separates request acceptance from order completion.
type CancellationResult struct {
	Request CancellationSnapshot
	Order   IntentSnapshot
}

// CancellationRepository commits commands/outbox and handles unsent intents
// atomically; observations never directly release a sent order's reservation.
type CancellationRepository interface {
	Accept(ctx context.Context, input CancelInput, execution LeaseToken) (CancellationSnapshot, error)
	Prepare(ctx context.Context, current CancellationSnapshot) (CancellationSnapshot, error)
	Observe(ctx context.Context, current CancellationSnapshot, state CancellationState, code string) (CancellationSnapshot, error)
	ByID(ctx context.Context, accountID string, id int64) (CancellationSnapshot, error)
}

// CancellationService queries original order identity before any cancellation.
type CancellationService interface {
	Cancel(ctx context.Context, input CancelInput, execution LeaseToken) (CancellationResult, error)
	Recover(ctx context.Context, accountID string, id int64) (CancellationResult, error)
}
