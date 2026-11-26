package rpcclient

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/nacos"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/testcert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

type poolDiscovery struct {
	mu        sync.Mutex
	instances []nacos.Instance
	failure   error
	block     <-chan struct{}
	entered   chan struct{}
	calls     int
}

func (d *poolDiscovery) GetService(ctx context.Context, _ string) ([]nacos.Instance, error) {
	d.mu.Lock()
	d.calls++
	rows, err, block := append([]nacos.Instance(nil), d.instances...), d.failure, d.block
	d.mu.Unlock()
	if d.entered != nil {
		select {
		case d.entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}
	return rows, err
}

type poolProbe struct {
	health.UnimplementedHealthServer
	status  health.HealthCheckResponse_ServingStatus
	calls   atomic.Int32
	wait    atomic.Bool
	entered chan struct{}
}

func (p *poolProbe) Check(ctx context.Context, _ *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	p.calls.Add(1)
	if p.wait.Load() {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return &health.HealthCheckResponse{Status: p.status}, nil
}

func poolNode(t *testing.T, ca *testcert.Authority, address string, probe *poolProbe) (string, func()) {
	t.Helper()
	security, err := rpcsecurity.Server(&config.GRPCServerSecurity{
		Mtls:                ca.Issue(t, "execution.grpc", false),
		AllowedPeerServices: []string{"api.grpc"},
	}, "execution.grpc")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)))
	health.RegisterHealthServer(server, probe)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			server.Stop()
			if err := <-done; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Error(err)
			}
		})
	}
	t.Cleanup(stop)
	return listener.Addr().String(), stop
}

func poolFactory(t *testing.T, ca *testcert.Authority, d *poolDiscovery) *Factory {
	t.Helper()
	f, err := New(t.Context(), &config.GRPCClients{
		Mtls:             ca.Issue(t, "api.grpc", false),
		ConnectTimeout:   durationpb.New(500 * time.Millisecond),
		RequestTimeout:   durationpb.New(2 * time.Second),
		MaxInflight:      32,
		MaxRequestBytes:  4096,
		MaxResponseBytes: 4096,
		AllowedServices:  []string{"execution.grpc"},
	}, "api.grpc", d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func poolClient(t *testing.T, f *Factory, id string) health.HealthClient {
	t.Helper()
	conn, err := f.Client(Target{
		Service:    "execution.grpc",
		InstanceID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	return health.NewHealthClient(conn)
}

func poolCheck(t *testing.T, client health.HealthClient, want health.HealthCheckResponse_ServingStatus) {
	t.Helper()
	result, err := client.Check(t.Context(), &health.HealthCheckRequest{})
	if err != nil || result.GetStatus() != want {
		t.Fatalf("Check = %v, %v, want %v", result, err, want)
	}
}

// Wait on publication, not sleeps or the discovery fetch notification: fetching
// a changed list is not yet the point at which new routing is installed.
func awaitRoute(t *testing.T, f *Factory, predicate func(*serviceRoute) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	for {
		f.mu.Lock()
		route := f.routes["execution.grpc"]
		ready := predicate(route)
		updated := route.updated
		f.mu.Unlock()
		if ready {
			return
		}
		f.requestRefresh("execution.grpc")
		select {
		case <-ctx.Done():
			t.Fatal("route publication did not complete")
		case <-updated:
		}
	}
}

func TestPoolAddressChangeRemovedOwnerAndDiscoveryFailure(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	a, b := &poolProbe{status: health.HealthCheckResponse_SERVING}, &poolProbe{status: health.HealthCheckResponse_NOT_SERVING}
	addrA, _ := poolNode(t, ca, "127.0.0.1:0", a)
	addrB, _ := poolNode(t, ca, "127.0.0.1:0", b)
	d := &poolDiscovery{instances: []nacos.Instance{
		{
			Service: "execution.grpc",
			ID:      "node-a",
			Version: "one",
			Address: addrA,
		},
	}}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "node-a")
	poolCheck(t, client, health.HealthCheckResponse_SERVING)
	d.mu.Lock()
	d.instances[0].Address = addrB
	d.instances[0].Version = "two"
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.instances[0].Address == addrB })
	poolCheck(t, client, health.HealthCheckResponse_NOT_SERVING)
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatal("changed address retained the old route")
	}
	d.mu.Lock()
	d.instances[0].ID = "node-b"
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.instances[0].ID == "node-b" })
	if _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); status.Code(err) != codes.Unavailable || b.calls.Load() != 1 {
		t.Fatalf("removed pinned owner fell back: %v calls=%d", err, b.calls.Load())
	}
	generic := poolClient(t, f, "")
	poolCheck(t, generic, health.HealthCheckResponse_NOT_SERVING)
	d.mu.Lock()
	d.failure = errors.New("discovery unavailable")
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.failure != nil })
	if _, err := generic.Check(t.Context(), &health.HealthCheckRequest{}); err == nil || b.calls.Load() != 2 {
		t.Fatalf("discovery failure used cached membership: %v", err)
	}
	d.mu.Lock()
	d.failure = nil
	d.instances = nil
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.failure == nil && len(r.instances) == 0 })
	if _, err := generic.Check(t.Context(), &health.HealthCheckRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("empty membership = %v", err)
	}
}

func TestPoolCoalescesColdDiscoveryAndCallerDoesNotOwnRefresh(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &poolProbe{status: health.HealthCheckResponse_SERVING}
	address, _ := poolNode(t, ca, "127.0.0.1:0", probe)
	release := make(chan struct{})
	d := &poolDiscovery{
		instances: []nacos.Instance{
			{
				Service: "execution.grpc",
				ID:      "node-a",
				Address: address,
			},
		},
		block:   release,
		entered: make(chan struct{}, 1),
	}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "")
	first, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() { _, err := client.Check(first, &health.HealthCheckRequest{}); cancelled <- err }()
	<-d.entered
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation = %v", err)
	}
	done := make(chan error, 16)
	for range 16 {
		go func() { _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); done <- err }()
	}
	close(release)
	for range 16 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	d.mu.Lock()
	lookups := d.calls
	d.mu.Unlock()
	if lookups >= 16 || probe.calls.Load() != 16 {
		t.Fatalf("coalesced discovery lookups=%d business calls=%d", lookups, probe.calls.Load())
	}
}

func TestPoolReconnectsSameEndpointWithoutReplaying(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	a := &poolProbe{status: health.HealthCheckResponse_SERVING}
	address, stop := poolNode(t, ca, "127.0.0.1:0", a)
	d := &poolDiscovery{instances: []nacos.Instance{
		{
			Service: "execution.grpc",
			ID:      "node-a",
			Address: address,
		},
	}}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "")
	poolCheck(t, client, health.HealthCheckResponse_SERVING)
	f.mu.Lock()
	var conn *grpc.ClientConn
	for _, entry := range f.connections {
		conn = entry.conn
	}
	f.mu.Unlock()
	stop()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for state := conn.GetState(); state == connectivity.Ready; state = conn.GetState() {
		if !conn.WaitForStateChange(ctx, state) {
			t.Fatal("transport did not notice disconnect")
		}
	}
	b := &poolProbe{status: health.HealthCheckResponse_NOT_SERVING}
	poolNode(t, ca, address, b)
	poolCheck(t, client, health.HealthCheckResponse_NOT_SERVING)
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatal("reconnect replayed a request")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.connections {
		if entry.conn != conn {
			t.Fatal("reconnect discarded the reusable client channel")
		}
	}
}

func TestPoolFailureCancelsOldTransportWithoutRetry(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &poolProbe{
		status:  health.HealthCheckResponse_SERVING,
		entered: make(chan struct{}, 1),
	}
	address, _ := poolNode(t, ca, "127.0.0.1:0", probe)
	d := &poolDiscovery{instances: []nacos.Instance{
		{
			Service: "execution.grpc",
			ID:      "node-a",
			Address: address,
		},
	}}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "")
	poolCheck(t, client, health.HealthCheckResponse_SERVING)
	probe.wait.Store(true)
	done := make(chan error, 1)
	go func() { _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); done <- err }()
	<-probe.entered
	d.mu.Lock()
	d.failure = errors.New("lookup failed")
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.failure != nil })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("old transport continued after failed membership")
		}
	case <-time.After(time.Second):
		t.Fatal("old transport was not closed")
	}
	if probe.calls.Load() != 2 {
		t.Fatalf("request replayed after retirement: calls=%d", probe.calls.Load())
	}
}

func TestPoolCloseJoinsBlockedDiscovery(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	d := &poolDiscovery{
		block:   make(chan struct{}),
		entered: make(chan struct{}, 1),
	}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "")
	done := make(chan error, 1)
	go func() { _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); done <- err }()
	<-d.entered
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
		t.Fatalf("shutdown of pending discovery = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.active) != 0 || len(f.connections) != 0 {
		t.Fatal("shutdown retained active owners or channels")
	}
}

func TestPoolRejectsStaleOrMalformedMembership(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &poolProbe{status: health.HealthCheckResponse_SERVING}
	address, _ := poolNode(t, ca, "127.0.0.1:0", probe)
	d := &poolDiscovery{instances: []nacos.Instance{
		{
			Service: "execution.grpc",
			ID:      "node-a",
			Address: address,
		},
	}}
	f := poolFactory(t, ca, d)
	client := poolClient(t, f, "")
	poolCheck(t, client, health.HealthCheckResponse_SERVING)
	// Model a refresh owner delayed beyond the maximum membership age. Routing
	// must fail before reaching a still-healthy, formerly valid transport.
	blocked := make(chan struct{})
	d.mu.Lock()
	d.block = blocked
	d.mu.Unlock()
	f.mu.Lock()
	f.routes["execution.grpc"].checkedAt = time.Now().Add(-membershipMaximumAge)
	f.mu.Unlock()
	if _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); status.Code(err) != codes.Unavailable || probe.calls.Load() != 1 {
		t.Fatalf("expired membership sent a call: %v calls=%d", err, probe.calls.Load())
	}
	d.mu.Lock()
	d.block = nil
	d.instances[0].Service = "market.grpc"
	d.mu.Unlock()
	close(blocked)
	awaitRoute(t, f, func(r *serviceRoute) bool { return r.failure != nil })
	if _, err := client.Check(t.Context(), &health.HealthCheckRequest{}); err == nil || probe.calls.Load() != 1 {
		t.Fatalf("wrong service membership reached the old provider: %v", err)
	}
}

func (p *poolProbe) Watch(_ *health.HealthCheckRequest, stream health.Health_WatchServer) error {
	if err := stream.Send(&health.HealthCheckResponse{Status: p.status}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return status.FromContextError(stream.Context().Err()).Err()
}

func TestPoolNodeRemovalEndsReadSubscription(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &poolProbe{status: health.HealthCheckResponse_SERVING}
	address, _ := poolNode(t, ca, "127.0.0.1:0", probe)
	d := &poolDiscovery{instances: []nacos.Instance{
		{
			Service: "execution.grpc",
			ID:      "node-a",
			Address: address,
		},
	}}
	f := poolFactory(t, ca, d)
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- f.Subscribe(t.Context(), Target{Service: "execution.grpc"}, func(ctx context.Context, conn grpc.ClientConnInterface) error {
			stream, err := health.NewHealthClient(conn).Watch(ctx, &health.HealthCheckRequest{})
			if err != nil {
				return err
			}
			if _, err := stream.Recv(); err != nil {
				return err
			}
			close(started)
			_, err = stream.Recv()
			return err
		})
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("stream startup = %v", err)
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	d.mu.Lock()
	d.instances = nil
	d.mu.Unlock()
	awaitRoute(t, f, func(r *serviceRoute) bool { return len(r.instances) == 0 })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("removed provider kept serving the read subscription")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not notice removal")
	}
}

func TestPoolCancelledSubscriptionDoesNotStartDiscovery(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	d := &poolDiscovery{}
	f := poolFactory(t, ca, d)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := f.Subscribe(ctx, Target{Service: "execution.grpc"}, func(context.Context, grpc.ClientConnInterface) error {
		t.Error("cancelled subscription reached consumer")
		return nil
	})
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancelled subscription = %v discoveries=%d, want cancelled without I/O", err, calls)
	}
}
