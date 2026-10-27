//go:build integration

package infrastructureprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAPIUsesDiscoveredAuthenticatedRPCClients(t *testing.T) {
	const serviceName = "rpc-factory-probe"
	ca := testcert.New(t)
	_, stopA, resultA := secureRPCNode(t, ca, serviceName, "secure-a")
	secureRPCNode(t, ca, serviceName, "secure-b")
	observer := namingObserver(t)
	waitNamingCount(t, observer, serviceName+".grpc", 2)
	cfg := serviceConfig("api-rpc-probe")
	cfg.Name = "api"
	cfg.Postgres = nil
	cfg.Grpc = nil
	cfg.Nacos = namingConfig()
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	cfg.GrpcClients = &configv1.GRPCClients{
		Mtls:             ca.Issue(t, "api.grpc", false),
		ConnectTimeout:   durationpb.New(500 * time.Millisecond),
		RequestTimeout:   durationpb.New(time.Second),
		MaxInflight:      2,
		MaxRequestBytes:  4096,
		MaxResponseBytes: 4096,
		AllowedServices:  []string{serviceName + ".grpc"},
	}
	api, cleanup, err := deer.NewDeerApi(t.Context(), cfg, deer.APISpec{
		Service: deer.ServiceSpec{LogOutput: io.Discard},
		Routes: func(_ context.Context, owner *deer.Deer, hertz *server.Hertz) error {
			factory, err := owner.GRPCClients()
			if err != nil {
				return err
			}
			hertz.GET("/probe/multiply", func(ctx context.Context, response *app.RequestContext) {
				connection, err := factory.Client(rpcclient.Target{
					Service:    serviceName + ".grpc",
					InstanceID: response.Query("instance"),
				})
				if err != nil {
					response.Status(http.StatusServiceUnavailable)
					return
				}
				var headers metadata.MD
				reply, err := probev1.NewArithmeticProbeClient(connection).Multiply(ctx, &probev1.MultiplyRequest{
					Price:    response.Query("price"),
					Quantity: response.Query("quantity"),
				}, grpc.Header(&headers))
				if err != nil {
					response.Status(http.StatusServiceUnavailable)
					return
				}
				identity := headers.Get(namingInstanceHeader)
				if len(identity) != 1 {
					response.Status(http.StatusBadGateway)
					return
				}
				response.Header(namingInstanceHeader, identity[0])
				response.JSON(http.StatusOK, reply)
			})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runNamingService(t, api.Deer, cleanup)
	for _, id := range []string{"secure-a", "secure-b"} {
		callAPIProbe(t, api.HTTPAddress(), id, http.StatusOK)
	}
	if err := stopA(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-resultA; err != nil {
		t.Fatal(err)
	}
	waitNamingCount(t, observer, serviceName+".grpc", 1)
	callAPIProbe(t, api.HTTPAddress(), "secure-a", http.StatusServiceUnavailable)
	callAPIProbe(t, api.HTTPAddress(), "secure-b", http.StatusOK)
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
}

func secureRPCNode(t *testing.T, ca *testcert.Authority, serviceName, id string) (*deer.Deer, deer.Cleanup, <-chan error) {
	t.Helper()
	cfg := serviceConfig(id)
	cfg.Name = serviceName
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(time.Second),
	}
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, serviceName+".grpc", false),
		AllowedPeerServices: []string{"api.grpc"},
	}
	return runNamingConfig(t, cfg, id)
}

func callAPIProbe(t *testing.T, address, instance string, wantStatus int) {
	t.Helper()
	query := url.Values{
		"instance": []string{instance},
		"price":    []string{"0.1"},
		"quantity": []string{"0.2"},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/probe/multiply?"+query.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
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
	if response.StatusCode != wantStatus {
		t.Fatalf("API RPC status = %d, want %d", response.StatusCode, wantStatus)
	}
	if wantStatus != http.StatusOK {
		return
	}
	var reply probev1.MultiplyReply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Notional != "0.02" || response.Header.Get(namingInstanceHeader) != instance {
		t.Fatalf("API RPC reply = %v, identity=%s", &reply, response.Header.Get(namingInstanceHeader))
	}
}
