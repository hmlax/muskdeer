package deer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/internal/testcert"
)

type rpcDiscoveryFixture struct {
	mu        sync.Mutex
	instances []nacos.Instance
	failure   error
	lookups   int
}

type rpcFactoryProbe struct {
	probev1.UnimplementedArithmeticProbeServer
	calls   atomic.Int32
	wait    bool
	entered chan struct{}
}

func TestRPCFactoryTypedResponseAndPayloadBound(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &identityProbe{observed: make(chan identitySnapshot, 1)}
	node := runRPCFactoryNode(t, ca, probe)
	factory := newRPCFactory(t, ca, &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: node.GRPCAddress(),
		},
	}}, time.Second)
	connection, err := factory.Client(rpcclient.Target{Service: "execution.grpc"})
	if err != nil {
		t.Fatal(err)
	}
	client := probev1.NewArithmeticProbeClient(connection)
	reply, err := client.Multiply(t.Context(), &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	})
	if err != nil || reply.GetNotional() != "0.02" {
		t.Fatalf("typed RPC = %v, %v", reply, err)
	}
	<-probe.observed
	_, err = client.Multiply(t.Context(), &probev1.MultiplyRequest{
		Price:    strings.Repeat("9", 8192),
		Quantity: "0.2",
	}, grpc.MaxCallSendMsgSize(16384))
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("caller overrode message bound: %v", err)
	}
	select {
	case <-probe.observed:
		t.Fatal("oversized request reached the business handler")
	default:
	}
}

func TestRPCFactoryAppliesConfiguredDeadline(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &rpcFactoryProbe{
		wait:    true,
		entered: make(chan struct{}, 1),
	}
	node := runRPCFactoryNode(t, ca, probe)
	factory := newRPCFactory(t, ca, &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: node.GRPCAddress(),
		},
	}}, 250*time.Millisecond)
	connection, err := factory.Client(rpcclient.Target{Service: "execution.grpc"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = probev1.NewArithmeticProbeClient(connection).Multiply(t.Context(), &probev1.MultiplyRequest{})
	if !errors.Is(err, context.DeadlineExceeded) || status.Code(err) != codes.DeadlineExceeded || probe.calls.Load() != 1 {
		t.Fatalf("configured deadline = %v, calls=%d", err, probe.calls.Load())
	}
}

func TestRPCFactoryRejectsInvalidLimitsWithoutIO(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*configv1.GRPCClients)
	}{
		{
			name: "no admission bound",
			change: func(cfg *configv1.GRPCClients) {
				cfg.MaxInflight = 0
			},
		},
		{
			name: "no certificate reference",
			change: func(cfg *configv1.GRPCClients) {
				cfg.Mtls = nil
			},
		},
		{
			name: "no payload bound",
			change: func(cfg *configv1.GRPCClients) {
				cfg.MaxRequestBytes = 0
			},
		},
		{
			name: "connection outlives call",
			change: func(cfg *configv1.GRPCClients) {
				cfg.ConnectTimeout = durationpb.New(2 * time.Second)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &configv1.GRPCClients{
				Mtls: &configv1.MutualTLS{
					CertificateFile: "unopened-certificate.pem",
					PrivateKeyFile:  "unopened-key.pem",
					RootCertFile:    "unopened-root.pem",
				},
				ConnectTimeout:   durationpb.New(time.Second),
				RequestTimeout:   durationpb.New(time.Second),
				MaxInflight:      1,
				MaxRequestBytes:  4096,
				MaxResponseBytes: 4096,
				AllowedServices:  []string{"execution.grpc"},
			}
			test.change(cfg)
			if err := rpcclient.Validate(cfg); err == nil {
				t.Fatal("invalid limits were accepted before I/O")
			}
		})
	}
}

func TestRPCFactoryFreshDiscoveryOwnerSelectionAndNoReplay(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &rpcFactoryProbe{}
	node := runRPCFactoryNode(t, ca, probe)
	discovery := &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: node.GRPCAddress(),
		},
	}}
	factory := newRPCFactory(t, ca, discovery, time.Second)
	connection, err := factory.Client(rpcclient.Target{
		Service:    "execution.grpc",
		InstanceID: "execution-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := probev1.NewArithmeticProbeClient(connection)
	_, err = client.Multiply(t.Context(), &probev1.MultiplyRequest{Price: "0.1"})
	if status.Code(err) != codes.Unavailable || probe.calls.Load() != 1 {
		t.Fatalf("failed business RPC replayed: status=%v, calls=%d", err, probe.calls.Load())
	}
	discovery.mu.Lock()
	discovery.instances[0].ID = "execution-b"
	discovery.mu.Unlock()
	_, err = client.Multiply(t.Context(), &probev1.MultiplyRequest{Price: "0.1"})
	if status.Code(err) != codes.Unavailable || probe.calls.Load() != 1 {
		t.Fatalf("missing owner fell back to another instance: %v, calls=%d", err, probe.calls.Load())
	}
	discovery.mu.Lock()
	discovery.failure = errors.New("fixture discovery unavailable")
	lookups := discovery.lookups
	discovery.mu.Unlock()
	_, err = client.Multiply(t.Context(), &probev1.MultiplyRequest{Price: "0.1"})
	if err == nil || probe.calls.Load() != 1 || lookups != 2 {
		t.Fatalf("lookup failure used a stale endpoint: %v, calls=%d, earlier lookups=%d", err, probe.calls.Load(), lookups)
	}
	if _, err := factory.Client(rpcclient.Target{Service: "market.grpc"}); err == nil {
		t.Fatal("factory allowed an unconfigured service")
	}
}

func TestRPCFactoryBoundsAdmissionDeadlineAndClose(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &rpcFactoryProbe{
		wait:    true,
		entered: make(chan struct{}, 1),
	}
	node := runRPCFactoryNode(t, ca, probe)
	discovery := &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: node.GRPCAddress(),
		},
	}}
	factory := newRPCFactory(t, ca, discovery, 500*time.Millisecond)
	connection, err := factory.Client(rpcclient.Target{Service: "execution.grpc"})
	if err != nil {
		t.Fatal(err)
	}
	client := probev1.NewArithmeticProbeClient(connection)
	active := make(chan error, 1)
	go func() {
		_, err := client.Multiply(t.Context(), &probev1.MultiplyRequest{})
		active <- err
	}()
	select {
	case <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("RPC did not reach its server")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err = client.Multiply(ctx, &probev1.MultiplyRequest{})
	if !errors.Is(err, context.DeadlineExceeded) || probe.calls.Load() != 1 {
		t.Fatalf("admission deadline = %v, calls=%d", err, probe.calls.Load())
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-active; !errors.Is(err, context.Canceled) {
		t.Fatalf("close did not cancel and join RPC: %v", err)
	}
	_, err = client.Multiply(t.Context(), &probev1.MultiplyRequest{})
	if !errors.Is(err, rpcclient.ErrClosed) {
		t.Fatalf("closed factory reopened: %v", err)
	}
}

func runRPCFactoryNode(t *testing.T, ca *testcert.Authority, probe probev1.ArithmeticProbeServer) *Deer {
	t.Helper()
	cfg := serviceConfig("execution-a")
	cfg.Name = "execution"
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, "execution.grpc", false),
		AllowedPeerServices: []string{"api.grpc"},
	}
	node, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		RegisterGRPC: func(_ context.Context, _ *Deer, registrar grpc.ServiceRegistrar) error {
			probev1.RegisterArithmeticProbeServer(registrar, probe)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, node)
	return node
}

func newRPCFactory(t *testing.T, ca *testcert.Authority, discovery rpcclient.Discovery, budget time.Duration) *rpcclient.Factory {
	t.Helper()
	factory, err := rpcclient.New(t.Context(), &configv1.GRPCClients{
		Mtls:             ca.Issue(t, "api.grpc", false),
		ConnectTimeout:   durationpb.New(200 * time.Millisecond),
		RequestTimeout:   durationpb.New(budget),
		MaxInflight:      1,
		MaxRequestBytes:  4096,
		MaxResponseBytes: 4096,
		AllowedServices:  []string{"execution.grpc"},
	}, "api.grpc", discovery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	return factory
}

func (d *rpcDiscoveryFixture) GetService(ctx context.Context, _ string) ([]nacos.Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookups++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]nacos.Instance(nil), d.instances...), d.failure
}

func (p *rpcFactoryProbe) Multiply(ctx context.Context, _ *probev1.MultiplyRequest) (*probev1.MultiplyReply, error) {
	p.calls.Add(1)
	if p.wait {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return nil, status.Error(codes.Unavailable, "fixture business failure")
}
