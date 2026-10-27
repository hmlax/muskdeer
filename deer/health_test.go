package deer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestHTTPAndGRPCHealthReflectDependencies(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("health-probe")
	cfg.Monitoring = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	service, _, err := NewDeer(t.Context(), cfg, ServiceSpec{LogOutput: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// The separate factory has its own test cleanup; close it to inject a real,
	// terminal dependency failure without mocking CheckReady's implementation.
	factory := newRPCFactory(t, testcert.New(t), &rpcDiscoveryFixture{}, time.Second)
	service.rpcClients = factory
	if service.Live() {
		t.Fatal("unstarted service reports live")
	}
	runDeer(t, service)
	client := healthProbeClient(t, service.GRPCAddress())
	assertHealthHTTP(t, service.MonitoringAddress(), "/livez", http.StatusOK, "alive")
	assertHealthHTTP(t, service.MonitoringAddress(), "/readyz", http.StatusOK, "ready")
	ctx, cancel := context.WithTimeout(t.Context(), 800*time.Millisecond)
	defer cancel()
	watch, err := client.Watch(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := watch.Recv(); err != nil || reply.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("initial Watch = %v, %v", reply, err)
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	assertHealthHTTP(t, service.MonitoringAddress(), "/readyz", http.StatusServiceUnavailable, "unready")
	assertHealthHTTP(t, service.MonitoringAddress(), "/livez", http.StatusOK, "alive")
	if reply, err := client.Check(ctx, &healthv1.HealthCheckRequest{}); err != nil || reply.GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("dependency failure Check = %v, %v", reply, err)
	}
	if reply, err := watch.Recv(); err != nil || reply.GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("dependency failure Watch = %v, %v", reply, err)
	}
	if reply, err := client.List(ctx, &healthv1.HealthListRequest{}); err != nil || len(reply.GetStatuses()) != 2 || reply.Statuses[""].GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("dependency failure List = %v, %v", reply, err)
	}
	if _, err := client.Check(ctx, &healthv1.HealthCheckRequest{Service: "unknown"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown health service = %v", err)
	}
	cancel()
	if err := service.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if service.Live() || service.Ready() {
		t.Fatal("stopped service reports live or ready")
	}
}

func TestGRPCHealthWatchAdmissionIsBounded(t *testing.T) {
	t.Parallel()
	service, _, err := NewDeer(t.Context(), serviceConfig("health-watch-capacity"), ServiceSpec{LogOutput: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, service)
	client := healthProbeClient(t, service.GRPCAddress())
	ctx, cancel := context.WithTimeout(t.Context(), 800*time.Millisecond)
	defer cancel()
	for range 4 {
		stream, err := client.Watch(ctx, &healthv1.HealthCheckRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := client.Watch(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("excess Watch admission = %v", err)
	}
}

func healthProbeClient(t *testing.T, address string) healthv1.HealthClient {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})
	return healthv1.NewHealthClient(conn)
}

func assertHealthHTTP(t *testing.T, address, path string, wantCode int, wantStatus string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantCode || body.Status != wantStatus {
		t.Fatalf("health %s = %d %v, want %d %s", path, response.StatusCode, body, wantCode, wantStatus)
	}
}
