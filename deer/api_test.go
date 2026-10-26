package deer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestDeerApiSharesDependenciesAndLifecycle(t *testing.T) {
	t.Parallel()
	var opened, released atomic.Int64
	resource := countedResource(&released)
	open := resource.Open
	resource.Open = func(ctx context.Context) (Cleanup, error) {
		opened.Add(1)
		return open(ctx)
	}
	api, cleanup, err := NewDeerApi(t.Context(), apiConfig("api-shared"), APISpec{
		Service: ServiceSpec{
			Resources: []Initializer{resource},
		},
		Routes: func(h *server.Hertz) error {
			h.GET("/probe", func(ctx context.Context, request *app.RequestContext) {
				if _, exists := ctx.Deadline(); !exists {
					t.Error("HTTP handlers must receive a bounded request context")
				}
				request.JSON(http.StatusOK, &probev1.MultiplyReply{Notional: "0.02"})
			})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := runDeer(t, api.Deer)
	assertGRPCHealthy(t, api.GRPCAddress())
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + api.HTTPAddress() + "/probe")
	if err != nil {
		t.Fatal(err)
	}
	var payload probev1.MultiplyReply
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	closeErr := response.Body.Close()
	client.CloseIdleConnections()
	if decodeErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || payload.Notional != "0.02" {
		t.Fatalf("HTTP probe = %v, status %d, errors %v/%v; want string notional", &payload, response.StatusCode, decodeErr, closeErr)
	}
	for range 2 {
		if err := cleanup(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 1 || released.Load() != 1 || api.Ready() {
		t.Fatalf("API resources = opened %d, released %d, ready %v; want 1/1/false", opened.Load(), released.Load(), api.Ready())
	}
	for _, address := range []string{api.GRPCAddress(), api.HTTPAddress()} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("listener %s not released: %v", address, err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeerApiRouteFailureRollsBackSharedResources(t *testing.T) {
	t.Parallel()
	failure := errors.New("route registration failed")
	var released atomic.Int64
	api, cleanup, err := NewDeerApi(t.Context(), apiConfig("route-failure"), APISpec{
		Service: ServiceSpec{
			Resources: []Initializer{countedResource(&released)},
		},
		Routes: func(*server.Hertz) error { return failure },
	})
	if api != nil || cleanup != nil || !errors.Is(err, failure) || released.Load() != 1 {
		t.Fatalf("route failure = %v, cleanup nil %v, %v, released %d; want rollback", api, cleanup == nil, err, released.Load())
	}
}

func TestDeerApiDrainsRequestBeforeReleasingDependencies(t *testing.T) {
	t.Parallel()
	testAPIDrain(t, false)
}

func TestDeerApiListenerFailureDrainsAcceptedRequest(t *testing.T) {
	t.Parallel()
	testAPIDrain(t, true)
}

func testAPIDrain(t *testing.T, failListener bool) {
	t.Helper()
	entered := make(chan struct{})
	finishRequest := make(chan struct{})
	var released atomic.Int64
	api, cleanup, err := NewDeerApi(t.Context(), apiConfig("api-drain"), APISpec{
		Service: ServiceSpec{
			Resources: []Initializer{countedResource(&released)},
		},
		Routes: func(h *server.Hertz) error {
			h.GET("/drain", func(ctx context.Context, request *app.RequestContext) {
				close(entered)
				select {
				case <-finishRequest:
					request.Status(http.StatusNoContent)
				case <-ctx.Done():
					request.Status(http.StatusRequestTimeout)
				}
			})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	runResult := runDeer(t, api.Deer)
	requestResult := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + api.HTTPAddress() + "/drain")
		if err == nil {
			err = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = errors.Join(err, errors.New("draining request did not complete"))
			}
		}
		client.CloseIdleConnections()
		requestResult <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not enter handler")
	}
	closeResult := make(chan error, 1)
	if failListener {
		if err := api.servers[1].listener.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		go func() { closeResult <- cleanup(t.Context()) }()
	}
	<-api.stopSignal
	if released.Load() != 0 {
		t.Error("dependencies released before accepted request finished")
	}
	close(finishRequest)
	if err := <-requestResult; err != nil {
		t.Fatal(err)
	}
	if err := <-runResult; (err != nil) != failListener {
		t.Fatalf("Run with listener failure %v = %v", failListener, err)
	}
	if failListener {
		if err := cleanup(t.Context()); err != nil {
			t.Fatal(err)
		}
	} else if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
	if released.Load() != 1 {
		t.Errorf("drained resource released %d times, want 1", released.Load())
	}
}

func TestDeerApiHTTPBindFailureRollsBackDeer(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	}()
	cfg := apiConfig("api-bind-failure")
	cfg.Http.Address = listener.Addr().String()
	var released atomic.Int64
	api, cleanup, err := NewDeerApi(t.Context(), cfg, APISpec{
		Service: ServiceSpec{
			Resources: []Initializer{countedResource(&released)},
		},
	})
	if api != nil || cleanup != nil || err == nil || released.Load() != 1 {
		t.Fatalf("occupied HTTP port = %v, cleanup nil %v, %v, released %d; want rollback", api, cleanup == nil, err, released.Load())
	}
}

func apiConfig(id string) *configv1.ServiceConfig {
	cfg := serviceConfig(id)
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	return cfg
}
