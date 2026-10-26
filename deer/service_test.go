package deer

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

type identitySnapshot struct {
	name    string
	id      string
	version string
}

type identityProbe struct {
	probev1.UnimplementedArithmeticProbeServer
	observed chan identitySnapshot
}

func (p *identityProbe) Multiply(ctx context.Context, request *probev1.MultiplyRequest) (*probev1.MultiplyReply, error) {
	identity, ok := kratos.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, "missing service identity")
	}
	p.observed <- identitySnapshot{
		name:    identity.Name(),
		id:      identity.ID(),
		version: identity.Version(),
	}
	price, err := decimal.NewFromString(request.Price)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid price")
	}
	quantity, err := decimal.NewFromString(request.Quantity)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid quantity")
	}
	return &probev1.MultiplyReply{Notional: price.Mul(quantity).String()}, nil
}

func TestDeerRegistersRPCWithImmutableServiceIdentity(t *testing.T) {
	t.Parallel()
	probe := &identityProbe{observed: make(chan identitySnapshot, 1)}
	cfg := serviceConfig("identity")
	service, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		RegisterGRPC: func(server grpc.ServiceRegistrar) error {
			probev1.RegisterArithmeticProbeServer(server, probe)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Name = "caller-mutated-name"
	result := runDeer(t, service)
	conn, err := grpc.NewClient(service.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := probev1.NewArithmeticProbeClient(conn).Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	})
	if err != nil || reply.GetNotional() != "0.02" {
		t.Fatalf("registered Multiply = %v, %v; want exact 0.02", reply, err)
	}
	identity := <-probe.observed
	if identity.name != "muskdeer-test" || identity.id != "identity" || identity.version != "test" {
		t.Fatalf("RPC service identity = %+v; want original constructor identity", identity)
	}
	if err := service.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDeerIndependentInstances(t *testing.T) {
	t.Parallel()
	var released atomic.Int64
	newInstance := func(id string) (*Deer, Cleanup) {
		t.Helper()
		cfg := serviceConfig(id)
		service, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{
			Resources: []Initializer{countedResource(&released)},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := cleanup(t.Context()); err != nil {
				t.Error(err)
			}
		})
		// Constructor input is borrowed only during the call.
		cfg.Name = "changed-after-construction"
		return service, cleanup
	}
	first, closeFirst := newInstance("first")
	second, closeSecond := newInstance("second")
	firstResult := runDeer(t, first)
	secondResult := runDeer(t, second)
	for _, service := range []*Deer{first, second} {
		assertGRPCHealthy(t, service.GRPCAddress())
	}
	if first.GRPCAddress() == second.GRPCAddress() {
		t.Fatal("independent instances must own different listeners")
	}
	if err := closeFirst(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	assertGRPCHealthy(t, second.GRPCAddress())
	if !second.Ready() || first.Ready() || released.Load() != 1 {
		t.Fatal("closing first instance must not stop or release second instance")
	}
	if err := closeSecond(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	if released.Load() != 2 {
		t.Errorf("released resources = %d, want 2", released.Load())
	}
}

func TestDeerConstructionFailureReleasesResources(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := serviceConfig("occupied-port")
	cfg.Grpc.Address = listener.Addr().String()
	var released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
	})
	if err == nil || service != nil || cleanup != nil || released.Load() != 1 {
		t.Fatalf("occupied port = (%v, cleanup nil %v, %v), released %d; want rollback", service, cleanup == nil, err, released.Load())
	}
}

func TestDeerRegistrationFailureReleasesListener(t *testing.T) {
	t.Parallel()
	failure := errors.New("RPC registration failed")
	var released atomic.Int64
	_, _, err := NewDeer(t.Context(), serviceConfig("registration-failure"), ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
		RegisterGRPC: func(grpc.ServiceRegistrar) error {
			return failure
		},
	})
	if !errors.Is(err, failure) || released.Load() != 1 {
		t.Fatalf("registration failure = %v, released %d; want original error and rollback", err, released.Load())
	}
}

func TestDeerCanceledRunAndRepeatedCleanup(t *testing.T) {
	t.Parallel()
	var released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("canceled"), ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Run = %v, want context.Canceled", err)
	}
	for range 2 {
		if err := cleanup(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if released.Load() != 1 || service.Ready() {
		t.Errorf("canceled service = released %d, ready %v; want 1/false", released.Load(), service.Ready())
	}
	if err := service.Run(t.Context()); err == nil {
		t.Fatal("service must reject a second Run")
	}
}

func TestDeerCleanupBeforeRun(t *testing.T) {
	t.Parallel()
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("unused"), ServiceSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Run(t.Context()); err == nil {
		t.Fatal("Run after cleanup must fail")
	}
	listener, err := net.Listen("tcp", service.GRPCAddress())
	if err != nil {
		t.Fatalf("unused listener was not released: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeerCanceledRunningServiceReleasesResources(t *testing.T) {
	t.Parallel()
	var released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("running-cancel"), ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.Run(ctx) }()
	ready, readyCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer readyCancel()
	if err := service.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("running service cancellation = %v, want context.Canceled", err)
	}
	if released.Load() != 1 || service.Ready() {
		t.Fatal("Run must finish cleanup before returning cancellation")
	}
}

func TestDeerConcurrentCleanup(t *testing.T) {
	t.Parallel()
	var released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("concurrent-cleanup"), ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			if err := cleanup(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	callers.Wait()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if released.Load() != 1 {
		t.Fatalf("concurrent service cleanup released %d times, want 1", released.Load())
	}
}

func TestDeerTransportFailureStopsService(t *testing.T) {
	t.Parallel()
	var released atomic.Int64
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("transport-failure"), ServiceSpec{
		Resources: []Initializer{countedResource(&released)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := runDeer(t, service)
	if err := service.servers[0].listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("unexpected listener failure must propagate from Run")
	}
	if released.Load() != 1 || service.Ready() {
		t.Fatal("transport failure must clear readiness and release resources")
	}
}

func TestDeerSupportsTaskOnlyLifecycle(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("task-only")
	cfg.Grpc = nil
	service, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	if service.GRPCAddress() != "" {
		t.Fatal("task-only service must not open a gRPC listener")
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDeerStreamsRequireBoundedNativeDeadline(t *testing.T) {
	t.Parallel()
	service, _, err := NewDeer(t.Context(), serviceConfig("bounded-stream"), ServiceSpec{})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	conn, err := grpc.NewClient(service.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	client := grpc_health_v1.NewHealthClient(conn)
	unbounded, cancelUnbounded := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancelUnbounded()
	for _, ctx := range []context.Context{unbounded, withTestDeadline(t, 2*time.Second)} {
		stream, err := client.Watch(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("unbounded/overlong Watch = %v; want InvalidArgument", err)
		}
	}
	stream, err := client.Watch(withTestDeadline(t, 500*time.Millisecond), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("bounded Watch expiration = %v; want DeadlineExceeded", err)
	}
	if err := service.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func withTestDeadline(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	t.Cleanup(cancel)
	return ctx
}

func serviceConfig(id string) *configv1.ServiceConfig {
	return &configv1.ServiceConfig{
		Name:            "muskdeer-test",
		InstanceId:      id,
		Version:         "test",
		StartupTimeout:  durationpb.New(3 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
	}
}

func countedResource(released *atomic.Int64) Initializer {
	return Initializer{
		Name: "counted",
		Open: func(context.Context) (Cleanup, error) {
			return func(context.Context) error {
				released.Add(1)
				return nil
			}, nil
		},
	}
}

func runDeer(t *testing.T, service *Deer) <-chan error {
	t.Helper()
	t.Cleanup(func() {
		if err := service.cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertGRPCHealthy(t *testing.T, address string) {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil || response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health Check = %v, %v; want SERVING", response, err)
	}
}
