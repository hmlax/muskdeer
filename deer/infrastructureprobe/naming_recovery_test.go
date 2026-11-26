//go:build integration

package infrastructureprobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
)

func TestNacosPublicationRecoversWithoutProcessRestart(t *testing.T) {
	const serviceName = "naming-recovery-probe"
	cfg := serviceConfig(t, "publication-recovery")
	cfg.Name = serviceName
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	target, err := url.Parse("http://" + cfg.Nacos.Address)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxy.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	var outage atomic.Bool
	var heldBeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/ns/instance/beat" && outage.Load() {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			heldBeats.Add(1)
			<-r.Context().Done()
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	cfg.Nacos.Address = strings.TrimPrefix(server.URL, "http://")
	cfg.Nacos.CommandTimeout = durationpb.New(time.Second)
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(50 * time.Millisecond),
	}
	node, _, result := runNamingConfig(t, cfg, cfg.InstanceId)
	observer := namingObserver(t)
	initial := waitNamingCount(t, observer, serviceName+".grpc", 1)[0]
	connection, err := grpc.NewClient("passthrough:///"+node.GRPCAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})

	outage.Store(true)
	// Cross the unchanged 10-second readiness window using only this probe's
	// heartbeat proxy. Shared Nacos and every live business endpoint stay up.
	window := time.NewTimer(12 * time.Second)
	defer window.Stop()
	select {
	case err := <-result:
		t.Fatalf("heartbeat outage terminated the same process: %v", err)
	case <-window.C:
	}
	if heldBeats.Load() < 2 || heldBeats.Load() > 10 || !node.Live() || !node.Ready() {
		t.Fatalf("outage lost lifecycle ownership or amplified probes: attempts=%d", heldBeats.Load())
	}
	checkNamingHealth(t, connection, healthv1.HealthCheckResponse_NOT_SERVING)

	// Model ephemeral expiry by deleting only this isolated probe endpoint.
	// Recovery must restore the same ID/version/address without restarting Run.
	host, port, err := net.SplitHostPort(initial.Address)
	if err != nil {
		t.Fatal(err)
	}
	original := namingConfig()
	namingAdminRequest(t, http.MethodDelete, "/nacos/v2/ns/instance", url.Values{
		"namespaceId": []string{original.NamespaceId},
		"groupName":   []string{original.Group},
		"clusterName": []string{original.Cluster},
		"serviceName": []string{serviceName + ".grpc"},
		"ip":          []string{host},
		"port":        []string{port},
		"ephemeral":   []string{"true"},
	})
	outage.Store(false)
	awaitNamingRecovery(t, node.CheckReady, result)
	checkNamingHealth(t, connection, healthv1.HealthCheckResponse_SERVING)
	restored := waitNamingCount(t, observer, serviceName+".grpc", 1)[0]
	if restored != initial || node.GRPCAddress() != initial.Address {
		t.Fatal("recovery changed process endpoint or registration identity")
	}
	callNamingProbe(t, restored)
}

func checkNamingHealth(t *testing.T, connection *grpc.ClientConn, want healthv1.HealthCheckResponse_ServingStatus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := healthv1.NewHealthClient(connection).Check(ctx, &healthv1.HealthCheckRequest{})
	if err != nil || reply.GetStatus() != want {
		t.Fatalf("health=%v error=%v; want %v", reply, err, want)
	}
}

func awaitNamingRecovery(t *testing.T, check func(context.Context) error, result <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for check(ctx) != nil {
		select {
		case err := <-result:
			t.Fatalf("recovery terminated its process: %v", err)
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Nacos recovery did not restore readiness")
		}
	}
}
