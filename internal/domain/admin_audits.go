package domain

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// AdminAuditFilter uses exact matches, never SQL patterns or caller expressions.
// Empty fields mean all values within the current viewer's management scope.
type AdminAuditFilter struct {
	Page     AdminPage
	Actor    string
	Action   string
	Resource string
}

// Validate rejects unbounded pages and ambiguous or non-displayable filters.
func (f AdminAuditFilter) Validate() error {
	if f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize ||
		!auditFilterText(f.Actor, 64) || !auditFilterText(f.Action, 64) || !auditFilterText(f.Resource, 128) {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminAuditEntry is immutable history, without credentials or idempotency keys.
// Optional versions preserve the difference between absent and a real revision.
type AdminAuditEntry struct {
	ID            int64
	Actor         string
	ActorRole     AdminRole
	Action        string
	Resource      string
	Reason        string
	BeforeVersion *int64
	AfterVersion  *int64
	CreatedAt     time.Time
}

// AdminAuditPage is bounded and ascending, using an exclusive identity cursor.
type AdminAuditPage struct {
	Items       []AdminAuditEntry
	NextAfterID int64
}

// AdminAudits revalidates current viewer authority in each read-only snapshot.
// It exposes history only; reading cannot complete or resubmit an operation.
type AdminAudits interface {
	List(context.Context, string, AdminAuditFilter) (AdminAuditPage, error)
	Detail(context.Context, string, int64) (AdminAuditEntry, error)
}

func auditFilterText(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
