//go:build integration

package infrastructureprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	probev1 "github.com/hmlax/muskdeer/deer/pb/common/probe/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/testcert"
)

func TestServiceUsesDiscoveredAuthenticatedRPCClients(t *testing.T) {
	const serviceName = "rpc-factory-probe"
	ca := testcert.New(t)
	_, stopA, resultA := secureRPCNode(t, ca, serviceName, "secure-a")
	secureRPCNode(t, ca, serviceName, "secure-b")
	observer := namingObserver(t)
	waitNamingCount(t, observer, serviceName+".grpc", 2)
	cfg := serviceConfig(t, "api-rpc-probe")
	cfg.Name = "client-probe"
	cfg.Postgres = nil
	cfg.Grpc = nil
	cfg.Nacos = namingConfig()
	cfg.Monitoring = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	cfg.GrpcClients = &configv1.GRPCClients{
		Mtls:             ca.Issue(t, "client-probe.grpc", false),
		ConnectTimeout:   durationpb.New(500 * time.Millisecond),
		RequestTimeout:   durationpb.New(time.Second),
		MaxInflight:      2,
		MaxRequestBytes:  4096,
		MaxResponseBytes: 4096,
		AllowedServices:  []string{serviceName + ".grpc"},
	}
	api, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{LogOutput: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	runNamingService(t, api, cleanup)
	assertMonitoringStatus(t, api.MonitoringAddress(), "/livez", http.StatusOK, "alive")
	assertMonitoringStatus(t, api.MonitoringAddress(), "/readyz", http.StatusOK, "ready")
	for _, id := range []string{"secure-a", "secure-b"} {
		callServiceProbe(t, api, id, http.StatusOK)
	}
	if err := stopA(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-resultA; err != nil {
		t.Fatal(err)
	}
	waitNamingCount(t, observer, serviceName+".grpc", 1)
	callServiceProbe(t, api, "secure-a", http.StatusServiceUnavailable)
	callServiceProbe(t, api, "secure-b", http.StatusOK)
	factory, err := api.GRPCClients()
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.CheckReady(t.Context()); !errors.Is(err, rpcclient.ErrClosed) {
		t.Fatalf("API readiness ignored a closed RPC dependency: %v", err)
	}
	assertMonitoringStatus(t, api.MonitoringAddress(), "/readyz", http.StatusServiceUnavailable, "unready")
	assertMonitoringStatus(t, api.MonitoringAddress(), "/livez", http.StatusOK, "alive")
}

func assertMonitoringStatus(t *testing.T, address, path string, wantCode int, wantState string) {
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
	if response.StatusCode != wantCode || body.Status != wantState {
		t.Fatalf("monitoring %s = %d %s", path, response.StatusCode, body.Status)
	}
}

func secureRPCNode(t *testing.T, ca *testcert.Authority, serviceName, id string) (*deer.Deer, deer.Cleanup, <-chan error) {
	t.Helper()
	cfg := serviceConfig(t, id)
	cfg.Name = serviceName
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(time.Second),
	}
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, serviceName+".grpc", false),
		AllowedPeerServices: []string{"client-probe.grpc"},
	}
	return runNamingConfig(t, cfg, id)
}

func callServiceProbe(t *testing.T, owner *deer.Deer, instance string, wantStatus int) {
	t.Helper()
	factory, err := owner.GRPCClients()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := factory.Client(rpcclient.Target{
		Service:    "rpc-factory-probe.grpc",
		InstanceID: instance,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	id, err := requestid.New(ctx, "integration-"+instance)
	if err != nil {
		t.Fatal(err)
	}
	ctx = requestid.Bind(ctx, id)
	var headers metadata.MD
	reply, err := probev1.NewArithmeticProbeClient(conn).Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	}, grpc.Header(&headers))
	if wantStatus == http.StatusServiceUnavailable {
		if status.Code(err) != codes.Unavailable {
			t.Fatal("missing instance did not fail closed", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	identity, correlation := headers.Get(namingInstanceHeader), headers.Get(requestid.Header)
	if reply.Notional != "0.02" || len(identity) != 1 || identity[0] != instance || len(correlation) != 1 || correlation[0] != id.String() {
		t.Fatal("RPC lost its exact decimal, instance or correlation identity")
	}
}
