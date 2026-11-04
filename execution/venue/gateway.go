package venue

import (
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// Config is connection setup passed to the injected gateway.
type Config = gateway.Config

// Admission identifies one durable order and its execution owner.
type Admission = gateway.Admission

// SubmissionGuard authorizes the final send using business state.
type SubmissionGuard = gateway.SubmissionGuard

// SubmitRequest is the venue-neutral order command.
type SubmitRequest = gateway.SubmitRequest

// Scope identifies an existing external fund and symbol.
type Scope = gateway.Scope

// CancelRequest targets one immutable external order ID.
type CancelRequest = gateway.CancelRequest

// CancelAdmission identifies one durable cancellation attempt.
type CancelAdmission = gateway.CancelAdmission

// CancellationGuard retains execution authority through cancellation.
type CancellationGuard = gateway.CancellationGuard

// ErrInvalidConfig, ErrInvalidSubmit and ErrInvalidSnapshot share gateway error identities.
var (
	ErrInvalidConfig    = gateway.ErrInvalidConfig
	ErrInvalidSubmit    = gateway.ErrInvalidSubmit
	ErrInvalidSnapshot  = gateway.ErrInvalidSnapshot
	errSDKNotDispatched = gateway.ErrNotDispatched
)

const maximumOperationTimeout = gateway.MaximumOperationTimeout

type gatewaySession struct{ gateway.Client }
