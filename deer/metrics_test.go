package deer

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestGRPCMetricsIncludeHandlerErrorsAndStreamRejections(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("grpc-metrics")
	cfg.Monitoring = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	probe := &identityProbe{observed: make(chan identitySnapshot, 2)}
	service, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(_ context.Context, _ *Deer, registrar grpc.ServiceRegistrar) error {
			probev1.RegisterArithmeticProbeServer(registrar, probe)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, service)
	conn, err := grpc.NewClient("passthrough:///"+service.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	client := probev1.NewArithmeticProbeClient(conn)
	if _, err := client.Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "invalid",
		Quantity: "0.2",
	}); err == nil {
		t.Fatal("invalid probe succeeded")
	}
	unbounded, cancelWatch := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancelWatch()
	stream, err := healthv1.NewHealthClient(conn).Watch(unbounded, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("unbounded stream succeeded")
	}
	output := readMetricsHTTP(t, service.MonitoringAddress(), "/metrics")
	if !strings.Contains(output, `deer_requests_total{transport="grpc",code="OK"} 1`) || !strings.Contains(output, `deer_requests_total{transport="grpc",code="InvalidArgument"} 2`) || !strings.Contains(output, `deer_request_errors_total{transport="grpc"} 2`) {
		t.Fatalf("gRPC interceptors did not observe outcomes: %s", output)
	}
}

func TestDeerMetricsAreIsolatedAndScrapesDoNotObserveThemselves(t *testing.T) {
	t.Parallel()
	apiA := runMetricsAPI(t, "metric-a")
	apiB := runMetricsAPI(t, "metric-b")
	for _, path := range []string{"/ok", "/bad", "/unknown/private-id"} {
		readMetricsHTTP(t, apiA.HTTPAddress(), path)
	}
	output := readMetricsHTTP(t, apiA.MonitoringAddress(), "/metrics")
	for _, sample := range []string{
		`deer_requests_total{transport="http",code="2xx"} 1`,
		`deer_requests_total{transport="http",code="4xx"} 2`,
		`deer_request_errors_total{transport="http"} 2`,
		`deer_live 1`,
		`deer_lifecycle_ready 1`,
	} {
		if !strings.Contains(output, sample+"\n") {
			t.Fatalf("missing server metric %s", sample)
		}
	}
	if strings.Contains(output, "private-id") || output != readMetricsHTTP(t, apiA.MonitoringAddress(), "/metrics") {
		t.Fatal("scrape created unbounded labels or counted itself")
	}
	if other := readMetricsHTTP(t, apiB.MonitoringAddress(), "/metrics"); !strings.Contains(other, `deer_request_duration_seconds_count{transport="http"} 0`) {
		t.Fatal("two Deer APIs share metric state")
	}
	var parser expfmt.TextParser
	if _, err := parser.TextToMetricFamilies(strings.NewReader(output)); err != nil {
		t.Fatal(err)
	}
}

func runMetricsAPI(t *testing.T, id string) *DeerApi {
	t.Helper()
	cfg := serviceConfig(id)
	cfg.Grpc = nil
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	cfg.Monitoring = cfg.Http
	api, _, err := NewDeerApi(t.Context(), cfg, APISpec{
		Service: ServiceSpec{LogOutput: io.Discard},
		Routes: func(_ context.Context, _ *Deer, hertz *server.Hertz) error {
			hertz.GET("/ok", func(_ context.Context, response *app.RequestContext) {
				response.String(http.StatusOK, "ok")
			})
			hertz.GET("/bad", func(_ context.Context, response *app.RequestContext) {
				response.String(http.StatusBadRequest, "bad request")
			})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, api.Deer)
	return api
}

func readMetricsHTTP(t *testing.T, address, path string) string {
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
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		t.Fatal(err)
	}
	if path == "/metrics" && (response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "version=0.0.4")) {
		t.Fatalf("metrics response: status %d, type %s", response.StatusCode, response.Header.Get("Content-Type"))
	}
	return string(data)
}
