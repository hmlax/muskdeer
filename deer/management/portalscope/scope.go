// Package portalscope carries the portal selection resolved by the admin boundary.
package portalscope

import (
	"context"
	"strings"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const (
	TenantCodeHeader   = "X-Tenant-Code"
	TenantIDMetadata   = "X-Gateway-Portal-Tenant"
	TenantCodeMetadata = "X-Gateway-Portal-Code"
	ErrorUnavailable   = "merchant is unavailable"
	errorInvalidCode   = "invalid merchant code header"
	errorScopeMismatch = "account does not belong to this admin portal"
)

type Scope struct {
	TenantID uint32
	Code     string
}

type contextKey struct{}

func NewContext(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, contextKey{}, scope)
}

func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(contextKey{}).(Scope)
	return scope, ok
}

func ParseCode(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", operatorV1.ErrorAdminBadRequest(errorInvalidCode)
	}
	code := strings.TrimSpace(values[0])
	if len(code) > 128 {
		return "", operatorV1.ErrorAdminBadRequest(errorInvalidCode)
	}
	for _, c := range code {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return "", operatorV1.ErrorAdminBadRequest(errorInvalidCode)
	}
	return code, nil
}

// CheckTenant binds authenticated identity to a resolved HTTP portal.
// Internal calls without an HTTP scope retain their trusted-service contract.
func CheckTenant(ctx context.Context, tenantID uint32) error {
	if scope, ok := FromContext(ctx); ok && scope.TenantID != tenantID {
		return operatorV1.ErrorAdminForbidden(errorScopeMismatch)
	}
	return nil
}

func CookieName(ctx context.Context, base string) string {
	if scope, ok := FromContext(ctx); ok && scope.Code != "" {
		return base + "_" + scope.Code
	}
	return base
}
