//go:build integration

package infrastructureprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

type namingFixtureMetadata struct {
	ID      string `json:"deer_instance_id"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

func TestNacosNamingClient(t *testing.T) {
	if os.Getenv("MUSKDEER_TEST_NACOS_PASSWORD") == "" {
		t.Fatal("isolated Nacos password is required; run make integration")
	}
	t.Run("authenticated fresh lookup and namespace isolation", testFreshNamingLookup)
	t.Run("authentication status hides password", testNamingAuthentication)
	t.Run("deer observes closed discovery dependency", testNamingReadiness)
	t.Run("API construction failure closes discovery", testNamingAPIRollback)
}

func testFreshNamingLookup(t *testing.T) {
	const serviceName = "naming-probe.grpc"
	const nodeID = "naming-node-a"
	cfg := namingConfig()
	client, err := nacos.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	metadata, err := json.Marshal(namingFixtureMetadata{
		ID:      nodeID,
		Kind:    "grpc",
		Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"namespaceId": []string{cfg.NamespaceId},
		"groupName":   []string{cfg.Group},
		"clusterName": []string{cfg.Cluster},
		"serviceName": []string{serviceName},
		"ip":          []string{"127.0.0.1"},
		"port":        []string{"19991"},
		"ephemeral":   []string{"true"},
		"metadata":    []string{string(metadata)},
	}
	// Only the isolated fixture uses management operations here. Application
	// registration/heartbeats and Deer publication are the next separate step.
	namingAdminRequest(t, http.MethodPost, "/nacos/v2/ns/instance", values)
	defer namingAdminRequest(t, http.MethodDelete, "/nacos/v2/ns/instance", values)
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for {
		instances, err := client.GetService(deadline, serviceName)
		if err != nil {
			t.Fatal(err)
		}
		if len(instances) == 1 {
			if instances[0].ID != nodeID || instances[0].Address != "127.0.0.1:19991" {
				t.Fatalf("fresh naming endpoint = %+v", instances)
			}
			break
		}
		select {
		case <-deadline.Done():
			t.Fatal("Nacos did not expose registered fixture")
		case <-ticks.C:
		}
	}
	other := namingConfig()
	other.NamespaceId = "integration-other"
	isolated, err := nacos.New(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := isolated.Close(); err != nil {
			t.Error(err)
		}
	}()
	if instances, err := isolated.GetService(t.Context(), serviceName); err != nil || len(instances) != 0 {
		t.Fatalf("other namespace lookup = %+v, %v", instances, err)
	}
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.GetService(t.Context(), serviceName); !errors.Is(err, nacos.ErrClosed) {
		t.Fatalf("closed naming client = %v", err)
	}
}

func testNamingAuthentication(t *testing.T) {
	const invalidPassword = "invalid_nacos_test_only"
	const reference = "MUSKDEER_TEST_INVALID_NACOS_PASSWORD"
	t.Setenv(reference, invalidPassword)
	cfg := namingConfig()
	cfg.PasswordEnv = reference
	client, err := nacos.New(t.Context(), cfg)
	var rejection *nacos.APIError
	if client != nil || !errors.As(err, &rejection) || rejection.HTTPStatus != http.StatusForbidden {
		t.Fatalf("Nacos authentication = %v, %v", client, err)
	}
	if strings.Contains(err.Error(), invalidPassword) {
		t.Fatal("Nacos authentication error exposed password")
	}
}

func testNamingReadiness(t *testing.T) {
	cfg := serviceConfig("naming-ready")
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	client, err := service.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); !errors.Is(err, nacos.ErrClosed) {
		t.Fatalf("closed discovery readiness = %v", err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func testNamingAPIRollback(t *testing.T) {
	cfg := serviceConfig("api-naming-rollback")
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	failure := errors.New("naming route assembly rejected")
	var discovery *nacos.Client
	api, cleanup, err := deer.NewDeerApi(t.Context(), cfg, deer.APISpec{
		Service: deer.ServiceSpec{
			LogOutput: io.Discard,
		},
		Routes: func(_ context.Context, owner *deer.Deer, _ *server.Hertz) error {
			var err error
			discovery, err = owner.Discovery()
			if err != nil {
				return err
			}
			return failure
		},
	})
	if api != nil || cleanup != nil || !errors.Is(err, failure) {
		t.Fatalf("API naming assembly = %v, cleanup nil %v, %v", api, cleanup == nil, err)
	}
	if discovery == nil || !errors.Is(discovery.Ping(t.Context()), nacos.ErrClosed) {
		t.Fatal("API assembly failure did not close discovery client")
	}
}

func namingAdminRequest(t *testing.T, method, path string, values url.Values) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	loginBody := url.Values{
		"username": []string{"nacos"},
		"password": []string{os.Getenv("MUSKDEER_TEST_NACOS_PASSWORD")},
	}
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	namingFixtureRequest(ctx, t, http.MethodPost, "/nacos/v1/auth/login", loginBody, &login)
	if login.AccessToken == "" {
		t.Fatal("Nacos fixture login returned no token")
	}
	values.Set("accessToken", login.AccessToken)
	var result struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	namingFixtureRequest(ctx, t, method, path, values, &result)
	if result.Code != 0 || result.Data != "ok" {
		t.Fatalf("Nacos fixture operation returned code %d, data %v", result.Code, result.Data)
	}
}

func namingFixtureRequest(ctx context.Context, t *testing.T, method, path string, values url.Values, destination any) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:55442"+path, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Nacos fixture HTTP status %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(destination); err != nil {
		t.Fatal(err)
	}
}

func namingConfig() *configv1.Nacos {
	return &configv1.Nacos{
		Address:        "127.0.0.1:55442",
		NamespaceId:    "integration-naming",
		Group:          "MUSKDEER_TEST",
		Cluster:        "TEST",
		Username:       "nacos",
		PasswordEnv:    "MUSKDEER_TEST_NACOS_PASSWORD",
		ConnectTimeout: durationpb.New(2 * time.Second),
		CommandTimeout: durationpb.New(time.Second),
		TlsEnabled:     proto.Bool(false),
		MaxInflight:    2,
	}
}
