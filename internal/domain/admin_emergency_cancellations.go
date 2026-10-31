package domain

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

// AdminEmergencyCancelScope is an explicit cancellation boundary. Account-wide
// and platform-wide actions are not inferred from a strategy confirmation.
type AdminEmergencyCancelScope string

// AdminEmergencyCancelState summarizes durable work, never current risk authority.
type AdminEmergencyCancelState string

// Emergency cancellation is distinct from single-order cancel, stop and flatten.
const (
	AdminEmergencyCancelScopeStrategy  AdminEmergencyCancelScope = "strategy"
	AdminEmergencyCancelAccepted       AdminEmergencyCancelState = "ACCEPTED"
	AdminEmergencyCancelSucceeded      AdminEmergencyCancelState = "SUCCEEDED"
	AdminEmergencyCancelFailed         AdminEmergencyCancelState = "FAILED"
	AdminEmergencyCancelAction                                   = "strategy.emergency_cancel"
	MessageInvalidAdminEmergencyCancel                           = "invalid strategy emergency cancellation request or progress"
)

// ErrInvalidAdminEmergencyCancel never includes submitted identities or reasons.
var ErrInvalidAdminEmergencyCancel = errors.New(MessageInvalidAdminEmergencyCancel)

// AdminEmergencyCancelRequest confirms a strategy and its independent risk
// revision. Storage must atomically block new risk and persist the scoped work;
// normalization alone neither closes risk nor sends cancellation commands.
type AdminEmergencyCancelRequest struct {
	Key                  string
	Scope                AdminEmergencyCancelScope
	StrategyID           int64
	ExpectedRiskRevision int64
	Reason               string
}

// Normalize rejects implicit scope expansion and keeps exact integer identity.
func (r AdminEmergencyCancelRequest) Normalize() (AdminEmergencyCancelRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Scope != AdminEmergencyCancelScopeStrategy || r.StrategyID <= 0 || r.ExpectedRiskRevision <= 0 || r.ExpectedRiskRevision == math.MaxInt64 ||
		len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) || !utf8.ValidString(r.Reason) || len(r.Reason) == 0 || len(r.Reason) > 256 || strings.ContainsRune(r.Reason, 0) {
		return AdminEmergencyCancelRequest{}, ErrInvalidAdminEmergencyCancel
	}
	return r, nil
}

// AdminEmergencyCancelProgress counts disjoint target outcomes after acceptance.
// An unknown order remains outside Terminal/Failed. PendingSubmissions also
// covers unresolved outgoing submits with no known OMS identity; bounded target
// enrollment must finish before an empty or apparently terminal page can finish.
// Failed means a cancellation attempt failed, never release of its reservation.
type AdminEmergencyCancelProgress struct {
	Targets            int64
	Terminal           int64
	Failed             int64
	PendingSubmissions int64
	EnrollmentComplete bool
}

// State derives only historical work progress. Terminal may include a filled
// order; success neither restores risk, implies CANCELED, nor flattens inventory.
func (p AdminEmergencyCancelProgress) State() (AdminEmergencyCancelState, error) {
	if p.Targets < 0 || p.Terminal < 0 || p.Failed < 0 || p.PendingSubmissions < 0 ||
		p.Terminal > p.Targets || p.Failed > p.Targets-p.Terminal {
		return "", ErrInvalidAdminEmergencyCancel
	}
	if !p.EnrollmentComplete || p.PendingSubmissions != 0 || p.Terminal+p.Failed != p.Targets {
		return AdminEmergencyCancelAccepted, nil
	}
	if p.Failed != 0 {
		return AdminEmergencyCancelFailed, nil
	}
	return AdminEmergencyCancelSucceeded, nil
}
