package domain

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// AdminControlAction identifies an explicit management intent, not a fill or
// an authority to modify the core ledger. Cancel/close-position are separate.
type AdminControlAction string

// AdminOperationState separates accepted intent from observed completion.
type AdminOperationState string

// Fixed operation identifiers and safe public failures.
const (
	AdminControlStart              AdminControlAction  = "strategy.start"
	AdminControlResume             AdminControlAction  = "strategy.resume"
	AdminControlPause              AdminControlAction  = "strategy.pause"
	AdminControlStop               AdminControlAction  = "strategy.stop"
	AdminOperationAccepted         AdminOperationState = "ACCEPTED"
	AdminOperationSucceeded        AdminOperationState = "SUCCEEDED"
	AdminOperationSuperseded       AdminOperationState = "SUPERSEDED"
	AdminOperationOutcomePending                       = "pending"
	AdminOperationOutcomeReached                       = "target_reached"
	AdminOperationOutcomeReplaced                      = "target_replaced"
	AdminAuditResourceStrategy                         = "strategy_instance"
	MessageInvalidAdminControl                         = "invalid strategy management request"
	MessageAdminConflict                               = "management request conflicts with persisted state"
	MessageAdminControlUnavailable                     = "strategy management temporarily unavailable"
)

// Management mutation errors never contain the submitted key or reason.
var (
	ErrInvalidAdminControl     = errors.New(MessageInvalidAdminControl)
	ErrAdminConflict           = errors.New(MessageAdminConflict)
	ErrAdminControlUnavailable = errors.New(MessageAdminControlUnavailable)
)

// AdminControlRequest has an actor-scoped key and an explicit optimistic revision.
type AdminControlRequest struct {
	Key              string
	StrategyID       int64
	ExpectedRevision int64
	Action           AdminControlAction
	Reason           string
}

// Normalize validates canonical identity before storage or authority lookup.
func (r AdminControlRequest) Normalize() (AdminControlRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.StrategyID <= 0 || r.ExpectedRevision <= 0 || r.ExpectedRevision == math.MaxInt64 ||
		len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) || !utf8.ValidString(r.Reason) || len(r.Reason) == 0 || len(r.Reason) > 256 || strings.ContainsRune(r.Reason, 0) {
		return AdminControlRequest{}, ErrInvalidAdminControl
	}
	if _, valid := r.Action.Target(); !valid {
		return AdminControlRequest{}, ErrInvalidAdminControl
	}
	return r, nil
}

// Target maps a command to its durable desired state; actual state stays owned
// by the current fenced strategy controller.
func (a AdminControlAction) Target() (StrategyState, bool) {
	switch a {
	case AdminControlStart, AdminControlResume:
		return StrategyStateRunning, true
	case AdminControlPause:
		return StrategyStatePaused, true
	case AdminControlStop:
		return StrategyStateStopped, true
	default:
		return "", false
	}
}

// AdminOperation is a durable command receipt/result, without authentication secrets.
type AdminOperation struct {
	ID               int64
	ActorUserID      int64
	StrategyID       int64
	Action           AdminControlAction
	Target           StrategyState
	ExpectedRevision int64
	EffectRevision   int64
	ConfigVersion    int64
	Source           MarketSource
	State            AdminOperationState
	Outcome          string
	Reason           string
	Revision         int64
	CreatedAt        time.Time
	CompletedAt      time.Time
}

// AdminStrategyControl revalidates operator/CSRF inside the mutation transaction.
// Control only writes desired intent, the operation and an immutable audit.
type AdminStrategyControl interface {
	Control(context.Context, string, string, AdminControlRequest) (AdminOperation, error)
	Operation(context.Context, string, int64) (AdminOperation, error)
}

// AdminOperationObserver only projects authoritative runtime observations into
// accepted operation results; it cannot change desired state, risk or funds.
type AdminOperationObserver interface {
	Pending(context.Context, int64, int) ([]int64, error)
	Observe(context.Context, int64) (bool, error)
}
