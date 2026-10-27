//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

const namingInstanceHeader = "x-muskdeer-instance-id"

type namingProbeServer struct {
	probev1.UnimplementedArithmeticProbeServer
	instance string
}

func TestNacosPublicationLifecycle(t *testing.T) {
	t.Run("two nodes survive expiry threshold and serve RPC", testTwoNamingNodes)
	t.Run("lost registration stops its owner without recreation", testLostNamingNode)
}

func testTwoNamingNodes(t *testing.T) {
	const serviceName = "naming-lifecycle-probe"
	nodeA, stopA, resultA := runNamingNode(t, serviceName, "publication-a", time.Second)
	nodeB, _, _ := runNamingNode(t, serviceName, "publication-b", time.Second)
	if nodeA.GRPCAddress() == nodeB.GRPCAddress() {
		t.Fatal("independent instances share one reserved address")
	}
	observer := namingObserver(t)
	instances := waitNamingCount(t, observer, serviceName+".grpc", 2)
	for _, instance := range instances {
		callNamingProbe(t, instance)
	}
	control := nacos.Registration{
		ID:      "unmaintained-control",
		Service: "naming-lifecycle-control.grpc",
		Version: "test",
		IP:      "127.0.0.1",
		Port:    19992,
	}
	if err := observer.Register(t.Context(), control); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := observer.Deregister(ctx, control); err != nil {
			t.Error(err)
		}
	}()
	waitNamingCount(t, observer, control.Service, 1)
	// Cross the default 15s unhealthy threshold and observe an unmaintained
	// control actually disappear. Server health checks run asynchronously.
	proof, cancelProof := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancelProof()
	started := time.Now()
	ticks := time.NewTicker(time.Second)
	defer ticks.Stop()
	checking := true
	for checking {
		select {
		case <-ticks.C:
			if err := nodeA.CheckReady(t.Context()); err != nil {
				failNamingReady(t, err)
			}
			if err := nodeB.CheckReady(t.Context()); err != nil {
				failNamingReady(t, err)
			}
			if instances, err := observer.GetService(t.Context(), serviceName+".grpc"); err != nil || len(instances) != 2 {
				t.Fatalf("heartbeat visibility = %+v, %v", instances, err)
			}
			unmaintained, err := observer.GetService(proof, control.Service)
			if err != nil {
				t.Fatal(err)
			}
			checking = len(unmaintained) != 0 || time.Since(started) < 16*time.Second
		case <-proof.Done():
			t.Fatal("server did not expire the unmaintained control within budget")
		}
	}
	if err := stopA(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-resultA; err != nil {
		t.Fatal(err)
	}
	remaining := waitNamingCount(t, observer, serviceName+".grpc", 1)
	if remaining[0].ID != "publication-b" {
		t.Fatalf("remaining instance = %+v", remaining)
	}
	callNamingProbe(t, remaining[0])
}

func failNamingReady(t *testing.T, err error) {
	t.Helper()
	var api *nacos.APIError
	if errors.As(err, &api) {
		t.Logf("Nacos status=%d code=%d", api.HTTPStatus, api.Code)
	}
	t.Fatalf("readiness=%v deadline=%v canceled=%v cause=%T", err,
		errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Unwrap(err))
}

func testLostNamingNode(t *testing.T) {
	const serviceName = "naming-loss-probe"
	node, _, result := runNamingNode(t, serviceName, "publication-lost", 100*time.Millisecond)
	observer := namingObserver(t)
	instances := waitNamingCount(t, observer, serviceName+".grpc", 1)
	host, port, err := net.SplitHostPort(instances[0].Address)
	if err != nil {
		t.Fatal(err)
	}
	cfg := namingConfig()
	namingAdminRequest(t, http.MethodDelete, "/nacos/v2/ns/instance", url.Values{
		"namespaceId": []string{cfg.NamespaceId},
		"groupName":   []string{cfg.Group},
		"clusterName": []string{cfg.Cluster},
		"serviceName": []string{serviceName + ".grpc"},
		"ip":          []string{host},
		"port":        []string{port},
		"ephemeral":   []string{"true"},
	})
	select {
	case err := <-result:
		var cause *nacos.APIError
		if !errors.As(err, &cause) || cause.Code != 20404 {
			t.Fatalf("lost registration Run = %v; want missing instance", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("node kept running after losing registration")
	}
	if node.Ready() {
		t.Fatal("terminated node still ready")
	}
	waitNamingCount(t, observer, serviceName+".grpc", 0)
}

func runNamingNode(t *testing.T, serviceName, id string, interval time.Duration) (*deer.Deer, deer.Cleanup, <-chan error) {
	t.Helper()
	cfg := serviceConfig(t, id)
	cfg.Name = serviceName
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(interval),
	}
	return runNamingConfig(t, cfg, id)
}

func runNamingConfig(t *testing.T, cfg *configv1.ServiceConfig, id string) (*deer.Deer, deer.Cleanup, <-chan error) {
	t.Helper()
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
			probev1.RegisterArithmeticProbeServer(registrar, &namingProbeServer{instance: id})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, cleanup, runNamingService(t, service, cleanup)
}

func runNamingService(t *testing.T, service *deer.Deer, cleanup deer.Cleanup) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(t.Context()))
	go func() {
		result <- service.Run(runCtx)
		close(result)
	}()
	t.Cleanup(func() {
		defer cancelRun()
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
		if err := <-result; err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		select {
		case runErr := <-result:
			t.Logf("service run ended before readiness: %v", runErr)
			if runErr != nil {
				failNamingReady(t, runErr)
			}
		default:
		}
		t.Fatal(err)
	}
	return result
}

func namingObserver(t *testing.T) *nacos.Client {
	t.Helper()
	client, err := nacos.New(t.Context(), namingConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func waitNamingCount(t *testing.T, client *nacos.Client, serviceName string, count int) []nacos.Instance {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	lastCount := -1
	for {
		instances, err := client.GetService(ctx, serviceName)
		if err != nil {
			t.Fatalf("discovery lookup for %s failed: last count=%d want=%d deadline=%v error=%v", serviceName, lastCount, count, errors.Is(err, context.DeadlineExceeded), err)
		}
		lastCount = len(instances)
		if len(instances) == count {
			return instances
		}
		select {
		case <-ctx.Done():
			t.Fatalf("discovery has %d nodes, want %d", len(instances), count)
		case <-ticks.C:
		}
	}
}

func callNamingProbe(t *testing.T, instance nacos.Instance) {
	t.Helper()
	// Plaintext is confined to the dedicated loopback-only integration probe.
	conn, err := grpc.NewClient("passthrough:///"+instance.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDisableRetry())
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
	var headers metadata.MD
	reply, err := probev1.NewArithmeticProbeClient(conn).Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	}, grpc.Header(&headers))
	if err != nil || reply.GetNotional() != "0.02" || len(headers.Get(namingInstanceHeader)) != 1 || headers.Get(namingInstanceHeader)[0] != instance.ID {
		t.Fatalf("discovered RPC for %s = %v, %v, header %v", instance.ID, reply, err, headers)
	}
}

func (s *namingProbeServer) Multiply(ctx context.Context, request *probev1.MultiplyRequest) (*probev1.MultiplyReply, error) {
	price, err := decimal.NewFromString(request.GetPrice())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid probe price")
	}
	quantity, err := decimal.NewFromString(request.GetQuantity())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid probe quantity")
	}
	if err := grpc.SetHeader(ctx, metadata.Pairs(namingInstanceHeader, s.instance)); err != nil {
		return nil, status.Error(codes.Internal, "probe identity header failed")
	}
	return &probev1.MultiplyReply{Notional: price.Mul(quantity).String()}, nil
}
