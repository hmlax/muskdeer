package domain

import (
	"context"
	"errors"
	"time"
)

// AdminRole is a management identity role, independent of trading authority.
type AdminRole string

// Management roles, safe public messages and bounded authentication policy.
const (
	AdminRoleViewer        AdminRole = "viewer"
	AdminRoleOperator      AdminRole = "operator"
	AdminRoleAdmin         AdminRole = "admin"
	MessageUnauthenticated           = "authentication required or invalid"
	MessageForbidden                 = "operation not permitted"
	MessageAuthUnavailable           = "authentication temporarily unavailable"
	MessageInvalidAuth               = "invalid authentication input"
	AdminSessionTTL                  = 8 * time.Hour
	AdminLockDuration                = 5 * time.Minute
	MaxAdminLoginFailures  int32     = 5
	MaxAdminSessions       int64     = 8
)

// Authentication failures never include credentials or persistence details.
var (
	ErrUnauthenticated = errors.New(MessageUnauthenticated)
	ErrForbidden       = errors.New(MessageForbidden)
	ErrAuthUnavailable = errors.New(MessageAuthUnavailable)
	ErrInvalidAuth     = errors.New(MessageInvalidAuth)
)

// Allows rejects unknown roles and capabilities rather than defaulting open.
func (r AdminRole) Allows(required AdminRole) bool {
	switch required {
	case AdminRoleViewer:
		return r == AdminRoleViewer || r == AdminRoleOperator || r == AdminRoleAdmin
	case AdminRoleOperator:
		return r == AdminRoleOperator || r == AdminRoleAdmin
	case AdminRoleAdmin:
		return r == AdminRoleAdmin
	default:
		return false
	}
}

// AdminPrincipal contains no password, session token or CSRF secret.
type AdminPrincipal struct {
	UserID    int64
	Username  string
	Role      AdminRole
	SessionID int64
	ExpiresAt time.Time
}

// AdminLogin returns secrets only at the initial authenticated boundary.
type AdminLogin struct {
	Principal AdminPrincipal
	Token     string
	CSRF      string
}

// AdminAuth borrows storage; provisioning is an explicit operator action.
// Identity is a point-in-time observation. Management writes must revalidate
// session and role inside their own transaction at the protected effect.
type AdminAuth interface {
	Provision(context.Context, string, string, string, AdminRole) (AdminPrincipal, error)
	Login(context.Context, string, string) (AdminLogin, error)
	Identity(context.Context, string) (AdminPrincipal, error)
	Authorize(context.Context, string, string, AdminRole) (AdminPrincipal, error)
	Logout(context.Context, string, string) error
}

// Management authentication audit vocabulary is shared by storage and delivery.
const (
	AdminAuditActionProvision      = "auth.provision"
	AdminAuditActionLogin          = "auth.login"
	AdminAuditActionLoginDenied    = "auth.login_denied"
	AdminAuditActionLogout         = "auth.logout"
	AdminAuditActionSessionEvicted = "auth.session_evicted"
	AdminAuditResourceUser         = "admin_user"
	AdminAuditResourceSession      = "admin_session"
	AdminAuditReasonProvision      = "explicit local identity provisioning"
	AdminAuditReasonLogin          = "credentials verified"
	AdminAuditReasonDenied         = "invalid credentials"
	AdminAuditReasonLogout         = "session revoked"
	AdminAuditReasonEvicted        = "oldest active session retired at session limit"
)
