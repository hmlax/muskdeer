// Package serviceoverview observes existing resources without owning any
// connection, registration, lease, worker, background loop or shutdown hook.
package serviceoverview

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

const (
	observationTimeout = 3 * time.Second
	authorityTimeout   = 400 * time.Millisecond
	discoveryTimeout   = 400 * time.Millisecond
	probeTimeout       = 350 * time.Millisecond
	maximumAllowlist   = 16
)

// IdentityReader checks current persistent session and role.
type IdentityReader interface {
	Identity(context.Context, string) (domain.AdminPrincipal, error)
}

// Clients borrows Deer's existing discovered/mTLS factory.
type Clients interface {
	Client(rpcclient.Target) (grpc.ClientConnInterface, error)
}

// LocalAPI borrows the current API's own readiness check and fixed metadata.
type LocalAPI struct {
	InstanceID string
	Version    string
	CheckReady func(context.Context) error
}

// Settings snapshots the existing allowlist. Now, when supplied for tests,
// must be safe for concurrent calls; nil uses time.Now.
type Settings struct {
	AllowedServices []string
	Now             func() time.Time
}

type observer struct {
	auth      IdentityReader
	discovery rpcclient.Discovery
	clients   Clients
	local     LocalAPI
	allowed   map[string]bool
	now       func() time.Time
}

// New starts no work, changes no allowlist and opens no connection.
func New(auth IdentityReader, discovery rpcclient.Discovery, clients Clients, local LocalAPI, settings Settings) (domain.AdminServices, error) {
	if auth == nil || discovery == nil || clients == nil || local.CheckReady == nil ||
		!domain.ValidAdminServiceLabel(local.InstanceID) || !domain.ValidAdminServiceLabel(local.Version) || len(settings.AllowedServices) > maximumAllowlist {
		return nil, domain.ErrInvalidAdminQuery
	}
	allowed := make(map[string]bool, len(settings.AllowedServices))
	for _, service := range settings.AllowedServices {
		allowed[service] = true
	}
	now := settings.Now
	if now == nil {
		now = time.Now
	}
	return &observer{
		auth:      auth,
		discovery: discovery,
		clients:   clients,
		local:     local,
		allowed:   allowed,
		now:       now,
	}, nil
}

// Observe admits only a current viewer and rechecks authority after all probes.
// Individual probe failures remain explicit diagnostics; discovery/authority
// failure or parent cancellation discards the whole observation.
func (o *observer) Observe(ctx context.Context, token string, filter domain.AdminServiceFilter) (domain.AdminServiceOverview, error) {
	if ctx == nil || filter.Validate() != nil {
		return domain.AdminServiceOverview{}, domain.ErrInvalidAdminQuery
	}
	call, cancel := context.WithTimeout(ctx, observationTimeout)
	defer cancel()
	first, err := o.authority(call, token)
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	value, err := o.observe(call, filter)
	if err != nil {
		return domain.AdminServiceOverview{}, observationFailure(call, err)
	}
	last, err := o.authority(call, token)
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	if first.UserID != last.UserID || first.SessionID != last.SessionID {
		return domain.AdminServiceOverview{}, domain.ErrUnauthenticated
	}
	if err := call.Err(); err != nil {
		return domain.AdminServiceOverview{}, err
	}
	return value, nil
}

func (o *observer) authority(ctx context.Context, token string) (domain.AdminPrincipal, error) {
	if err := ctx.Err(); err != nil {
		return domain.AdminPrincipal{}, err
	}
	call, cancel := context.WithTimeout(ctx, authorityTimeout)
	defer cancel()
	principal, err := o.auth.Identity(call, token)
	if err != nil {
		return domain.AdminPrincipal{}, observationFailure(call, err)
	}
	if err := call.Err(); err != nil {
		return domain.AdminPrincipal{}, err
	}
	if principal.UserID <= 0 || principal.SessionID <= 0 {
		return domain.AdminPrincipal{}, domain.ErrUnauthenticated
	}
	if !principal.Role.Allows(domain.AdminRoleViewer) {
		return domain.AdminPrincipal{}, domain.ErrForbidden
	}
	return principal, nil
}

func observationFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, domain.ErrUnauthenticated) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrAuthUnavailable) {
		return err
	}
	return errors.Join(domain.ErrAdminUnavailable, err)
}

func (o *observer) observe(ctx context.Context, filter domain.AdminServiceFilter) (domain.AdminServiceOverview, error) {
	if filter.Role == domain.AdminServiceAPI {
		return o.observeLocal(ctx, filter)
	}
	service := remoteService(filter.Role)
	call, cancel := context.WithTimeout(ctx, discoveryTimeout)
	instances, err := o.discovery.GetService(call, service)
	if err == nil {
		err = call.Err()
	}
	cancel()
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	if !validInventory(instances, service) {
		return domain.AdminServiceOverview{}, domain.ErrAdminUnavailable
	}
	now, err := o.sample()
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	instances = slices.Clone(instances)
	slices.SortFunc(instances, func(a, b nacos.Instance) int { return strings.Compare(a.ID, b.ID) })
	result := domain.AdminServiceOverview{
		Role:                 filter.Role,
		Source:               domain.AdminServiceRegistry,
		MembershipObservedAt: now,
		Total:                int64(len(instances)),
		ProbeConfigured:      o.allowed[service],
		Items:                make([]domain.AdminServiceInstance, 0, filter.Limit),
	}
	for _, instance := range instances {
		if instance.ID <= filter.AfterInstanceID {
			continue
		}
		if len(result.Items) == filter.Limit {
			result.NextAfterInstanceID = result.Items[len(result.Items)-1].InstanceID
			break
		}
		item := domain.AdminServiceInstance{
			InstanceID:      instance.ID,
			DeclaredVersion: instance.Version,
			Status:          domain.AdminServiceProbeNotConfigured,
		}
		if result.ProbeConfigured {
			item.Status = o.probe(ctx, service, instance.ID)
			observed, err := o.sample()
			if err != nil {
				return domain.AdminServiceOverview{}, err
			}
			item.ProbedAt = &observed
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (o *observer) observeLocal(ctx context.Context, filter domain.AdminServiceFilter) (domain.AdminServiceOverview, error) {
	now, err := o.sample()
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	result := domain.AdminServiceOverview{
		Role:                 domain.AdminServiceAPI,
		Source:               domain.AdminServiceLocalAPI,
		MembershipObservedAt: now,
		Total:                1,
		ProbeConfigured:      true,
		Items:                make([]domain.AdminServiceInstance, 0, 1),
	}
	if o.local.InstanceID <= filter.AfterInstanceID {
		return result, nil
	}
	call, cancel := context.WithTimeout(ctx, probeTimeout)
	err = o.local.CheckReady(call)
	probeErr := call.Err()
	cancel()
	state := domain.AdminServiceReady
	if probeErr != nil {
		state = domain.AdminServiceProbeTimeout
	} else if err != nil {
		state = domain.AdminServiceNotReady
	}
	checked, err := o.sample()
	if err != nil {
		return domain.AdminServiceOverview{}, err
	}
	result.Items = append(result.Items, domain.AdminServiceInstance{
		InstanceID:      o.local.InstanceID,
		DeclaredVersion: o.local.Version,
		Status:          state,
		ProbedAt:        &checked,
	})
	return result, nil
}

func (o *observer) probe(ctx context.Context, service, instanceID string) domain.AdminServiceProbeStatus {
	if ctx.Err() != nil {
		return domain.AdminServiceProbeFailed
	}
	connection, err := o.clients.Client(rpcclient.Target{
		Service:    service,
		InstanceID: instanceID,
	})
	if err != nil || connection == nil {
		return domain.AdminServiceProbeFailed
	}
	call, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	reply, err := healthv1.NewHealthClient(connection).Check(call, &healthv1.HealthCheckRequest{Service: service})
	if call.Err() != nil || status.Code(err) == codes.DeadlineExceeded {
		return domain.AdminServiceProbeTimeout
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return domain.AdminServiceProbeDenied
	case codes.ResourceExhausted:
		return domain.AdminServiceProbeCapacity
	case codes.OK:
		if reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 {
			return domain.AdminServiceProbeFailed
		}
		return healthStatus(reply.Status)
	default:
		return domain.AdminServiceProbeFailed
	}
}

func healthStatus(value healthv1.HealthCheckResponse_ServingStatus) domain.AdminServiceProbeStatus {
	switch value {
	case healthv1.HealthCheckResponse_SERVING:
		return domain.AdminServiceReady
	case healthv1.HealthCheckResponse_NOT_SERVING:
		return domain.AdminServiceNotReady
	case healthv1.HealthCheckResponse_UNKNOWN, healthv1.HealthCheckResponse_SERVICE_UNKNOWN:
		return domain.AdminServiceUnknown
	default:
		return domain.AdminServiceProbeFailed
	}
}

func remoteService(role domain.AdminServiceRole) string {
	switch role {
	case domain.AdminServiceMarket:
		return rpcsecurity.ServiceMarket
	case domain.AdminServiceStrategy:
		return rpcsecurity.ServiceStrategy
	case domain.AdminServiceExecution:
		return rpcsecurity.ServiceExecution
	case domain.AdminServicePortfolio:
		return rpcsecurity.ServicePortfolio
	case domain.AdminServiceCron:
		return rpcsecurity.ServiceCron
	default:
		return ""
	}
}

func validInventory(instances []nacos.Instance, service string) bool {
	if len(instances) > domain.MaxAdminServiceInstances {
		return false
	}
	seen := make(map[string]struct{}, len(instances))
	for _, item := range instances {
		if item.Service != service || !remoteInstanceID(item.ID) || !domain.ValidAdminServiceLabel(item.Version) {
			return false
		}
		if _, exists := seen[item.ID]; exists {
			return false
		}
		seen[item.ID] = struct{}{}
	}
	return true
}

func remoteInstanceID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func (o *observer) sample() (time.Time, error) {
	value := o.now().UTC().Truncate(time.Millisecond)
	if value.UnixMilli() <= 0 || !value.Equal(time.UnixMilli(value.UnixMilli())) {
		return time.Time{}, domain.ErrAdminUnavailable
	}
	return value, nil
}

var _ domain.AdminServices = (*observer)(nil)
