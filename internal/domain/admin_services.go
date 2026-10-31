package domain

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// AdminServiceRole selects a fixed platform role, never a caller-supplied host.
type AdminServiceRole string

// Platform service roles exposed by the management observer.
const (
	AdminServiceAPI          AdminServiceRole = "api"
	AdminServiceMarket       AdminServiceRole = "market"
	AdminServiceStrategy     AdminServiceRole = "strategy"
	AdminServiceExecution    AdminServiceRole = "execution"
	AdminServicePortfolio    AdminServiceRole = "portfolio"
	AdminServiceCron         AdminServiceRole = "cron"
	MaxAdminServicePage                       = 4
	MaxAdminServiceInstances                  = 1024
)

// Valid accepts only the six platform roles.
func (r AdminServiceRole) Valid() bool {
	switch r {
	case AdminServiceAPI, AdminServiceMarket, AdminServiceStrategy, AdminServiceExecution, AdminServicePortfolio, AdminServiceCron:
		return true
	default:
		return false
	}
}

// AdminServiceFilter uses a bounded ascending instance-ID cursor within one role.
type AdminServiceFilter struct {
	Role            AdminServiceRole
	AfterInstanceID string
	Limit           int
}

// Validate never accepts addresses or arbitrary service names as probe targets.
func (f AdminServiceFilter) Validate() error {
	if !f.Role.Valid() || f.Limit < 1 || f.Limit > MaxAdminServicePage ||
		(f.AfterInstanceID != "" && !ValidAdminServiceLabel(f.AfterInstanceID)) {
		return ErrInvalidAdminQuery
	}
	return nil
}

// ValidAdminServiceLabel bounds display metadata and local API identities.
// Remote instance IDs additionally pass the existing RPC target validation.
func ValidAdminServiceLabel(value string) bool {
	return len(value) > 0 && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

// AdminServiceObservationSource describes where membership was sampled.
type AdminServiceObservationSource string

// Membership is either this API or healthy/enabled gRPC registrations only.
const (
	AdminServiceLocalAPI AdminServiceObservationSource = "LOCAL_API"
	AdminServiceRegistry AdminServiceObservationSource = "NACOS_HEALTHY_GRPC"
)

// AdminServiceProbeStatus reports one bounded check, never execution ownership.
type AdminServiceProbeStatus string

// Fixed public probe outcomes do not expose provider errors or addresses.
const (
	AdminServiceReady              AdminServiceProbeStatus = "READY"
	AdminServiceNotReady           AdminServiceProbeStatus = "NOT_READY"
	AdminServiceUnknown            AdminServiceProbeStatus = "UNKNOWN"
	AdminServiceProbeNotConfigured AdminServiceProbeStatus = "PROBE_NOT_CONFIGURED"
	AdminServiceProbeFailed        AdminServiceProbeStatus = "PROBE_FAILED"
	AdminServiceProbeTimeout       AdminServiceProbeStatus = "PROBE_TIMEOUT"
	AdminServiceProbeDenied        AdminServiceProbeStatus = "PROBE_DENIED"
	AdminServiceProbeCapacity      AdminServiceProbeStatus = "PROBE_CAPACITY"
)

// AdminServiceInstance keeps declared version and actual readiness separate.
type AdminServiceInstance struct {
	InstanceID      string
	DeclaredVersion string
	Status          AdminServiceProbeStatus
	ProbedAt        *time.Time
}

// AdminServiceOverview is a sequence of observations, not an atomic cluster
// snapshot. Registry membership excludes unhealthy/disabled/non-gRPC endpoints.
// A subsequent RPC resolves its required instance again; membership and version
// do not establish process incarnation or trading authority.
type AdminServiceOverview struct {
	Role                 AdminServiceRole
	Source               AdminServiceObservationSource
	MembershipObservedAt time.Time
	Total                int64
	ProbeConfigured      bool
	Items                []AdminServiceInstance
	NextAfterInstanceID  string
}

// AdminServices rechecks the current viewer before and after bounded probes.
type AdminServices interface {
	Observe(context.Context, string, AdminServiceFilter) (AdminServiceOverview, error)
}
