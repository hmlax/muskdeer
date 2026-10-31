package serviceoverview

import (
	"context"
	"errors"
	"slices"
	"testing"
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

func TestObserverRejectsMissingBorrowedDependenciesWithoutStartingWork(t *testing.T) {
	if _, err := New(nil, nil, nil, LocalAPI{}, Settings{}); err == nil {
		t.Fatal("missing shared dependencies accepted")
	}
}

type identityFixture struct {
	calls int
	token string
	err   error
	hook  func(int) domain.AdminPrincipal
}

func (f *identityFixture) Identity(ctx context.Context, token string) (domain.AdminPrincipal, error) {
	f.calls++
	f.token = token
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > authorityTimeout {
		return domain.AdminPrincipal{}, errors.New("unbounded authority check")
	}
	principal := domain.AdminPrincipal{
		UserID:    1,
		SessionID: 2,
		Role:      domain.AdminRoleViewer,
	}
	if f.hook != nil {
		principal = f.hook(f.calls)
	}
	return principal, f.err
}

type discoveryFixture struct {
	calls     int
	service   string
	instances []nacos.Instance
	err       error
}

func (f *discoveryFixture) GetService(ctx context.Context, service string) ([]nacos.Instance, error) {
	f.calls++
	f.service = service
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > discoveryTimeout {
		return nil, errors.New("unbounded discovery")
	}
	return f.instances, f.err
}

type probeFixture struct {
	state   healthv1.HealthCheckResponse_ServingStatus
	err     error
	unknown bool
	hook    func(context.Context)
}

type clientsFixture struct {
	t       *testing.T
	targets []rpcclient.Target
	probes  map[string]probeFixture
	err     error
}

func (f *clientsFixture) Client(target rpcclient.Target) (grpc.ClientConnInterface, error) {
	f.targets = append(f.targets, target)
	if f.err != nil {
		return nil, f.err
	}
	return &healthConnection{
		fixture: f,
		target:  target,
	}, nil
}

type healthConnection struct {
	fixture *clientsFixture
	target  rpcclient.Target
}

func (c *healthConnection) Invoke(ctx context.Context, method string, request, reply any, _ ...grpc.CallOption) error {
	c.fixture.t.Helper()
	in, valid := request.(*healthv1.HealthCheckRequest)
	if !valid || method != healthv1.Health_Check_FullMethodName || in.Service != c.target.Service || c.target.InstanceID == "" {
		c.fixture.t.Fatal("probe used arbitrary method/service or stateless target")
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > probeTimeout {
		c.fixture.t.Fatal("unbounded health probe")
	}
	probe := c.fixture.probes[c.target.InstanceID]
	if probe.hook != nil {
		probe.hook(ctx)
	}
	out := reply.(*healthv1.HealthCheckResponse)
	out.Status = probe.state
	if probe.unknown {
		out.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
	}
	return probe.err
}

func (*healthConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("observer must not create streams")
}

func observerFixture(t *testing.T, auth *identityFixture, discovery *discoveryFixture, clients *clientsFixture, allowed []string) domain.AdminServices {
	t.Helper()
	reader, err := New(auth, discovery, clients, LocalAPI{
		InstanceID: "local-api",
		Version:    "test-example",
		CheckReady: func(context.Context) error { return nil },
	}, Settings{
		AllowedServices: allowed,
		Now:             func() time.Time { return time.UnixMilli(9007199254740993) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func inventoryFixture(service string) []nacos.Instance {
	return []nacos.Instance{
		{
			ID:      "node-z",
			Service: service,
			Version: "declared-z",
		},
		{
			ID:      "node-a",
			Service: service,
			Version: "declared-a",
		},
		{
			ID:      "node-m",
			Service: service,
			Version: "declared-m",
		},
	}
}

func TestObserverTargetsEveryNamedInstanceAndKeepsFullMembershipCountsAcrossPages(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{instances: inventoryFixture(rpcsecurity.ServiceStrategy)}
	before := slices.Clone(discovery.instances)
	clients := &clientsFixture{
		t: t,
		probes: map[string]probeFixture{
			"node-a": {state: healthv1.HealthCheckResponse_SERVING},
			"node-m": {state: healthv1.HealthCheckResponse_NOT_SERVING},
		},
	}
	allowed := []string{rpcsecurity.ServiceStrategy}
	reader := observerFixture(t, auth, discovery, clients, allowed)
	allowed[0] = rpcsecurity.ServiceMarket
	if auth.calls != 0 || discovery.calls != 0 || len(clients.targets) != 0 {
		t.Fatal("constructor started work")
	}
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 2,
	}
	first, err := reader.Observe(t.Context(), "test-token", filter)
	if err != nil || first.Total != 3 || !first.ProbeConfigured || first.Role != filter.Role || first.Source != domain.AdminServiceRegistry ||
		first.MembershipObservedAt.UnixMilli() != 9007199254740993 || len(first.Items) != 2 || first.Items[0].InstanceID != "node-a" || first.Items[0].DeclaredVersion != "declared-a" ||
		first.Items[0].Status != domain.AdminServiceReady || first.Items[1].Status != domain.AdminServiceNotReady || first.NextAfterInstanceID != "node-m" ||
		first.Items[0].ProbedAt == nil || first.Items[0].ProbedAt.UnixMilli() != 9007199254740993 || auth.calls != 2 || auth.token != "test-token" {
		t.Fatal("named probe lost bounded readiness, metadata, allowlist snapshot or viewer recheck", err)
	}
	assertObserverRemainingPages(t, reader, first.NextAfterInstanceID, clients, discovery, before)
}

func assertObserverRemainingPages(t *testing.T, reader domain.AdminServices, after string, clients *clientsFixture, discovery *discoveryFixture, before []nacos.Instance) {
	t.Helper()
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 2,
	}
	filter.AfterInstanceID = after
	last, err := reader.Observe(t.Context(), "test-token", filter)
	if err != nil || last.Total != 3 || len(last.Items) != 1 || last.Items[0].InstanceID != "node-z" || last.Items[0].Status != domain.AdminServiceUnknown || last.NextAfterInstanceID != "" {
		t.Fatal("last page narrowed discovery counts or converted UNKNOWN into ready", err)
	}
	filter.AfterInstanceID = "node-z"
	empty, err := reader.Observe(t.Context(), "test-token", filter)
	if err != nil || empty.Total != 3 || len(empty.Items) != 0 || empty.NextAfterInstanceID != "" || len(clients.targets) != 3 || discovery.calls != 3 || !slices.Equal(before, discovery.instances) {
		t.Fatal("cursor fabricated providers, rechecked unrelated instances or mutated discovery ownership", err)
	}
}

func TestObserverUnconfiguredProbesDoNotExpandFactoryPermissionsOrInventCheckTimes(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{instances: inventoryFixture(rpcsecurity.ServiceMarket)}
	clients := &clientsFixture{t: t}
	reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
	value, err := reader.Observe(t.Context(), "test-token", domain.AdminServiceFilter{
		Role:  domain.AdminServiceMarket,
		Limit: 4,
	})
	if err != nil || value.ProbeConfigured || value.Total != 3 || len(value.Items) != 3 || discovery.service != rpcsecurity.ServiceMarket || len(clients.targets) != 0 {
		t.Fatal("observer expanded explicit RPC permissions", err)
	}
	for _, item := range value.Items {
		if item.Status != domain.AdminServiceProbeNotConfigured || item.ProbedAt != nil {
			t.Fatal("unperformed check gained a result or timestamp")
		}
	}
}

func TestObserverIndividualProbeFailuresRemainBoundedDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name     string
		probe    probeFixture
		expected domain.AdminServiceProbeStatus
	}{
		{
			name:     "transport failure",
			probe:    probeFixture{err: errors.New("private provider address")},
			expected: domain.AdminServiceProbeFailed,
		},
		{
			name:     "permission denied",
			probe:    probeFixture{err: status.Error(codes.PermissionDenied, "private peer")},
			expected: domain.AdminServiceProbeDenied,
		},
		{
			name:     "transport timeout",
			probe:    probeFixture{err: status.Error(codes.DeadlineExceeded, "private dial timeout")},
			expected: domain.AdminServiceProbeTimeout,
		},
		{
			name:     "capacity exhausted",
			probe:    probeFixture{err: status.Error(codes.ResourceExhausted, "private admission capacity")},
			expected: domain.AdminServiceProbeCapacity,
		},
		{
			name: "actual child deadline despite late nil error",
			probe: probeFixture{
				state: healthv1.HealthCheckResponse_SERVING,
				hook:  func(ctx context.Context) { <-ctx.Done() },
			},
			expected: domain.AdminServiceProbeTimeout,
		},
		{
			name:     "unknown enum",
			probe:    probeFixture{state: 99},
			expected: domain.AdminServiceProbeFailed,
		},
		{
			name: "unknown response field",
			probe: probeFixture{
				state:   healthv1.HealthCheckResponse_SERVING,
				unknown: true,
			},
			expected: domain.AdminServiceProbeFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := &identityFixture{}
			discovery := &discoveryFixture{instances: inventoryFixture(rpcsecurity.ServiceStrategy)[:1]}
			clients := &clientsFixture{
				t:      t,
				probes: map[string]probeFixture{"node-z": test.probe},
			}
			reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
			value, err := reader.Observe(t.Context(), "test-token", domain.AdminServiceFilter{
				Role:  domain.AdminServiceStrategy,
				Limit: 1,
			})
			if err != nil || len(value.Items) != 1 || value.Items[0].Status != test.expected || value.Items[0].ProbedAt == nil || auth.calls != 2 {
				t.Fatal("failed probe hid other diagnostics or skipped authority recheck", err)
			}
		})
	}
}

func TestObserverRevocationAndParentCancellationDiscardAllProbedData(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		auth := &identityFixture{}
		discovery := &discoveryFixture{instances: inventoryFixture(rpcsecurity.ServiceStrategy)}
		clients := &clientsFixture{
			t: t,
			probes: map[string]probeFixture{
				"node-a": {
					state: healthv1.HealthCheckResponse_SERVING,
					hook: func(context.Context) {
						if revoked {
							auth.err = domain.ErrUnauthenticated
						} else {
							cancel()
						}
					},
				},
			},
		}
		reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
		value, err := reader.Observe(ctx, "test-token", domain.AdminServiceFilter{
			Role:  domain.AdminServiceStrategy,
			Limit: 2,
		})
		cancel()
		if revoked && !errors.Is(err, domain.ErrUnauthenticated) || !revoked && !errors.Is(err, context.Canceled) ||
			value.Total != 0 || value.Items != nil || value.Role != "" || !value.MembershipObservedAt.IsZero() || value.NextAfterInstanceID != "" {
			t.Fatal("revoked or cancelled observation returned partially probed data", err)
		}
		if !revoked && (auth.calls != 1 || len(clients.targets) != 1) {
			t.Fatal("cancelled observation started another dependency call")
		}
	}
}

func TestObserverRejectsMalformedInventoryAndUnauthorizedCallsBeforeProbing(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{}
	clients := &clientsFixture{t: t}
	reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	for _, instances := range [][]nacos.Instance{
		{
			{
				ID:      "node",
				Service: rpcsecurity.ServiceMarket,
				Version: "test",
			},
		},
		{
			{
				ID:      "127.0.0.1:80",
				Service: rpcsecurity.ServiceStrategy,
				Version: "test",
			},
		},
		{
			{
				ID:      "node",
				Service: rpcsecurity.ServiceStrategy,
			},
		},
		{
			{
				ID:      "node",
				Service: rpcsecurity.ServiceStrategy,
				Version: "test\nprivate",
			},
		},
		{
			{
				ID:      "node",
				Service: rpcsecurity.ServiceStrategy,
				Version: "test",
			},
			{
				ID:      "node",
				Service: rpcsecurity.ServiceStrategy,
				Version: "test",
			},
		},
		make([]nacos.Instance, domain.MaxAdminServiceInstances+1),
	} {
		discovery.instances = instances
		if value, err := reader.Observe(t.Context(), "test-token", filter); !errors.Is(err, domain.ErrAdminUnavailable) || value.Items != nil || value.Total != 0 || len(clients.targets) != 0 {
			t.Fatal("invalid discovery snapshot became a probe target or partial page", err)
		}
	}
	private := errors.New("private discovery failure")
	discovery.err = private
	if _, err := reader.Observe(t.Context(), "test-token", filter); !errors.Is(err, private) || !errors.Is(err, domain.ErrAdminUnavailable) {
		t.Fatal("discovery failure lost its internal cause or safe failure category", err)
	}
	previous := discovery.calls
	auth.err = domain.ErrUnauthenticated
	if _, err := reader.Observe(t.Context(), "test-token", filter); !errors.Is(err, domain.ErrUnauthenticated) || discovery.calls != previous {
		t.Fatal("anonymous observation reached registry", err)
	}
}

func TestObserverLocalAPIChecksActualReadinessWithoutDiscoveringOtherAPIs(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{}
	clients := &clientsFixture{t: t}
	checks := 0
	reader, err := New(auth, discovery, clients, LocalAPI{
		InstanceID: "current-api",
		Version:    "declared-test",
		CheckReady: func(ctx context.Context) error {
			checks++
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded local readiness")
			}
			return errors.New("private dependency is not ready")
		},
	}, Settings{Now: func() time.Time { return time.UnixMilli(2000).Add(999 * time.Nanosecond) }})
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceAPI,
		Limit: 1,
	}
	value, err := reader.Observe(t.Context(), "test-token", filter)
	if err != nil || value.Total != 1 || value.Source != domain.AdminServiceLocalAPI || !value.ProbeConfigured || len(value.Items) != 1 || value.Items[0].InstanceID != "current-api" ||
		value.Items[0].Status != domain.AdminServiceNotReady || value.Items[0].ProbedAt.UnixMilli() != 2000 || checks != 1 || discovery.calls != 0 || len(clients.targets) != 0 {
		t.Fatal("local API readiness fabricated cluster membership or ignored dependencies", err)
	}
	filter.AfterInstanceID = "current-api"
	value, err = reader.Observe(t.Context(), "test-token", filter)
	if err != nil || value.Total != 1 || len(value.Items) != 0 || checks != 1 {
		t.Fatal("local cursor invented another API or repeated hidden checks", err)
	}
}

func TestObserverChangedSessionOrRoleAtFinalCheckReturnsNoMembership(t *testing.T) {
	for _, changeRole := range []bool{false, true} {
		auth := &identityFixture{hook: func(call int) domain.AdminPrincipal {
			p := domain.AdminPrincipal{
				UserID:    1,
				SessionID: 2,
				Role:      domain.AdminRoleViewer,
			}
			if call == 2 {
				if changeRole {
					p.Role = "retired-role"
				} else {
					p.SessionID++
				}
			}
			return p
		}}
		discovery := &discoveryFixture{}
		reader := observerFixture(t, auth, discovery, &clientsFixture{t: t}, nil)
		value, err := reader.Observe(t.Context(), "test-token", domain.AdminServiceFilter{
			Role:  domain.AdminServiceStrategy,
			Limit: 1,
		})
		if changeRole && !errors.Is(err, domain.ErrForbidden) || !changeRole && !errors.Is(err, domain.ErrUnauthenticated) ||
			value.Items != nil || value.Total != 0 || value.Role != "" || !value.MembershipObservedAt.IsZero() || auth.calls != 2 {
			t.Fatal("session replacement or removed role retained sampled membership", err)
		}
	}
}

func TestObserverEmptyRegistryKeepsTimeButDoesNotInventReadyProviders(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{}
	clients := &clientsFixture{t: t}
	reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
	value, err := reader.Observe(t.Context(), "test-token", domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	})
	if err != nil || value.Items == nil || len(value.Items) != 0 || value.Total != 0 || !value.ProbeConfigured || value.MembershipObservedAt.UnixMilli() <= 0 ||
		value.NextAfterInstanceID != "" || len(clients.targets) != 0 || auth.calls != 2 {
		t.Fatal("empty filtered registry became current readiness or lost observation", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	previousAuth, previousDiscovery := auth.calls, discovery.calls
	if value, err := reader.Observe(ctx, "test-token", domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}); !errors.Is(err, context.Canceled) || value.Items != nil || auth.calls != previousAuth || discovery.calls != previousDiscovery {
		t.Fatal("already cancelled caller started dependency calls", err)
	}
}

func TestObserverInvalidClockAndUnavailableFactoryNeverClaimReady(t *testing.T) {
	auth := &identityFixture{}
	discovery := &discoveryFixture{instances: inventoryFixture(rpcsecurity.ServiceStrategy)[:1]}
	clients := &clientsFixture{
		t:   t,
		err: errors.New("private factory closure"),
	}
	reader := observerFixture(t, auth, discovery, clients, []string{rpcsecurity.ServiceStrategy})
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	value, err := reader.Observe(t.Context(), "test-token", filter)
	if err != nil || len(value.Items) != 1 || value.Items[0].Status != domain.AdminServiceProbeFailed || value.Items[0].ProbedAt == nil {
		t.Fatal("unavailable factory became ready or unconfigured", err)
	}
	reader, err = New(auth, discovery, clients, LocalAPI{
		InstanceID: "test-api",
		Version:    "test-example",
		CheckReady: func(context.Context) error { return nil },
	}, Settings{
		AllowedServices: []string{rpcsecurity.ServiceStrategy},
		Now:             func() time.Time { return time.Time{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	previous := len(clients.targets)
	value, err = reader.Observe(t.Context(), "test-token", filter)
	if !errors.Is(err, domain.ErrAdminUnavailable) || value.Items != nil || !value.MembershipObservedAt.IsZero() || len(clients.targets) != previous {
		t.Fatal("invalid clock gained a fabricated observation or started probes", err)
	}
}
