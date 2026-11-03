package domain

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// AdminCancellationState describes the scoped request, independently of OMS.
type AdminCancellationState string

// Cancellation requests never change a strategy's desired lifecycle or budget.
const (
	AdminCancellationAccepted       AdminCancellationState = "ACCEPTED"
	AdminCancellationSucceeded      AdminCancellationState = "SUCCEEDED"
	AdminCancellationFailed         AdminCancellationState = "FAILED"
	AdminCancellationAction                                = "order.cancel"
	AdminCancellationPending                               = "pending"
	AdminCancellationTerminal                              = "order_terminal"
	AdminCancellationRejected                              = "cancel_rejected"
	AdminCancellationNotAccepted                           = "cancel_not_accepted"
	AdminCancellationDecisionPrefix                        = "admin-order-cancel:"
	AdminAuditResourceOrder                                = "strategy_order"
	MessageInvalidAdminCancellation                        = "invalid order cancellation request"
)

// ErrInvalidAdminCancellation excludes submitted content from public failures.
var ErrInvalidAdminCancellation = errors.New(MessageInvalidAdminCancellation)

// AdminCancellationRequest binds confirmation to one immutable OMS identity.
type AdminCancellationRequest struct {
	Key                   string
	StrategyID            int64
	OrderID               int64
	ExpectedOrderRevision int64
	Reason                string
}

// Normalize validates the actor-scoped key, order confirmation and reason.
func (r AdminCancellationRequest) Normalize() (AdminCancellationRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.StrategyID <= 0 || r.OrderID <= 0 || r.ExpectedOrderRevision <= 0 || r.ExpectedOrderRevision == math.MaxInt64 ||
		len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) || !utf8.ValidString(r.Reason) || len(r.Reason) == 0 || len(r.Reason) > 256 || strings.ContainsRune(r.Reason, 0) {
		return AdminCancellationRequest{}, ErrInvalidAdminCancellation
	}
	return r, nil
}

// AdminCancellationReceipt separates acceptance from a proven OMS terminal state.
// A filled order satisfies the terminal objective; it does not imply cancellation.
type AdminCancellationReceipt struct {
	ID                    int64
	ActorUserID           int64
	StrategyID            int64
	OrderID               int64
	ExpectedOrderRevision int64
	Reason                string
	State                 AdminCancellationState
	Outcome               string
	Revision              int64
	CreatedAt             time.Time
	CompletedAt           time.Time
	ObservedOrderRevision int64
	ObservedOrderState    OrderState
}

// AdminOrderCancellations authenticates management requests and historical reads.
type AdminOrderCancellations interface {
	AdminCancellationWriter
	AdminCancellationReader
}

// AdminCancellationWriter revalidates mutation authority inside the transaction.
type AdminCancellationWriter interface {
	Request(context.Context, string, string, AdminCancellationRequest) (AdminCancellationReceipt, error)
}

// AdminCancellationReader observes durable results with current viewer authority.
type AdminCancellationReader interface {
	Receipt(context.Context, string, int64, int64) (AdminCancellationReceipt, error)
}

// StrategyOrderCancellations prepares at most one action under current fencing.
// afterID only schedules work fairly; no authorization is held in that cursor.
type StrategyOrderCancellations interface {
	Prepare(context.Context, StrategyAuthority, int64) (AdminCancellationWork, error)
}

// AdminCancellationWork may contain a new PENDING action for the existing sender.
type AdminCancellationWork struct {
	ReceiptID int64
	Action    *StrategyAction
}
