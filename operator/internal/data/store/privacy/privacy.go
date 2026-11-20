// Package privacy carries explicit trusted system decisions used during login.
package privacy

import (
	"context"

	"github.com/tx7do/go-crud/viewer"
)

// Decision identifies an internal privacy decision.
type Decision uint8
type decisionKey struct{}

// Privacy decisions distinguish trusted bypass from explicit denial.
const (
	Allow Decision = iota + 1
	Deny
)

// DecisionContext attaches a trusted internal privacy decision.
func DecisionContext(ctx context.Context, decision Decision) context.Context {
	if decision == Allow {
		return viewer.WithSystemContext(ctx)
	}
	return context.WithValue(ctx, decisionKey{}, Deny)
}

// IsDenied reports whether the trusted privacy decision rejects access.
func IsDenied(ctx context.Context) bool {
	decision, _ := ctx.Value(decisionKey{}).(Decision)
	return decision == Deny
}
