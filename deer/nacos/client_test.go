package nacos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestFreshLookupFiltersUnsafeEndpointsAndSharesAuthentication(t *testing.T) {
	var logins atomic.Int32
	var queries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/auth/login" {
			logins.Add(1)
			if err := r.ParseForm(); err != nil || r.Form.Get("password") != "unit_test_only" {
				t.Error("login did not use configured password reference")
			}
			writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
			return
		}
		queries.Add(1)
		if r.URL.Path != "/nacos/v2/ns/instance/list" || r.URL.Query().Get("namespaceId") != "unit-test" || r.URL.Query().Get("groupName") != "MUSKDEER_TEST" || r.Header.Get("Authorization") != "Bearer unit_token_only" || r.URL.Query().Has("accessToken") {
			t.Error("lookup did not preserve scoped authentication")
		}
		writeFixture(t, w, r, `{"code":0,"data":{"hosts":[
{"ip":"127.0.0.1","port":8081,"healthy":true,"enabled":true,"weight":1.0,"clusterName":"TEST","metadata":{"kind":"grpc","version":"test","deer_instance_id":"node-a"}},
{"ip":"127.0.0.1","port":8082,"healthy":false,"enabled":true,"weight":1.0,"clusterName":"TEST","metadata":{"kind":"grpc","deer_instance_id":"unhealthy"}},
{"ip":"127.0.0.1","port":8083,"healthy":true,"enabled":false,"weight":1.0,"clusterName":"TEST","metadata":{"kind":"grpc","deer_instance_id":"disabled"}},
{"ip":"127.0.0.1","port":8084,"healthy":true,"enabled":true,"weight":0.0,"clusterName":"TEST","metadata":{"kind":"grpc","deer_instance_id":"zero-weight"}},
{"ip":"127.0.0.1","port":8085,"healthy":true,"enabled":true,"weight":1.0,"clusterName":"OTHER","metadata":{"kind":"grpc","deer_instance_id":"other-cluster"}},
{"ip":"127.0.0.1","port":8086,"healthy":true,"enabled":true,"weight":1.0,"clusterName":"TEST","metadata":{"kind":"http","deer_instance_id":"http"}}
]}}`)
	}))
	defer server.Close()
	client := openTestClient(t, server.URL)
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			instances, err := client.GetService(t.Context(), "probe.grpc")
			if err != nil || len(instances) != 1 || instances[0].ID != "node-a" || instances[0].Address != "127.0.0.1:8081" {
				t.Errorf("filtered lookup = %+v, %v", instances, err)
			}
		})
	}
	callers.Wait()
	if logins.Load() != 1 || queries.Load() != 9 {
		t.Fatalf("login/query counts = %d/%d; want 1/9 (no instance cache)", logins.Load(), queries.Load())
	}
}

func TestCloseCancelsInflightRequestAndRejectsReopen(t *testing.T) {
	entered := make(chan struct{})
	var hold atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/auth/login" {
			writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
			return
		}
		if hold.Load() {
			close(entered)
			<-r.Context().Done()
			return
		}
		writeFixture(t, w, r, `{"code":0,"data":{"hosts":[]}}`)
	}))
	defer server.Close()
	client := openTestClient(t, server.URL)
	hold.Store(true)
	done := make(chan error, 1)
	go func() {
		_, err := client.GetService(t.Context(), "probe.grpc")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("lookup did not enter server")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("closed in-flight lookup = %v", err)
	}
	if _, err := client.GetService(t.Context(), "probe.grpc"); !errors.Is(err, ErrClosed) {
		t.Fatalf("lookup after Close = %v", err)
	}
}

func TestInvalidConfigurationBeforeIO(t *testing.T) {
	for _, change := range []func(*configv1.Nacos){
		func(cfg *configv1.Nacos) { cfg.NamespaceId = "" },
		func(cfg *configv1.Nacos) { cfg.NamespaceId = "public" },
		func(cfg *configv1.Nacos) { cfg.MaxInflight = 0 },
		func(cfg *configv1.Nacos) { cfg.CommandTimeout = nil },
		func(cfg *configv1.Nacos) { cfg.PasswordEnv = "" },
		func(cfg *configv1.Nacos) { cfg.Address = "user@127.0.0.1:8848" },
	} {
		cfg := testConfig("127.0.0.1:8848")
		change(cfg)
		if err := validate(cfg); err == nil {
			t.Fatal("invalid naming configuration accepted")
		}
	}
}

func TestAuthenticationFailureKeepsSafeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeFixture(t, w, r, "unit_test_only secret server response")
	}))
	defer server.Close()
	t.Setenv("MUSKDEER_UNIT_NACOS_PASSWORD", "unit_test_only")
	client, err := New(t.Context(), testConfig(strings.TrimPrefix(server.URL, "http://")))
	var apiError *APIError
	if client != nil || !errors.As(err, &apiError) || apiError.HTTPStatus != http.StatusForbidden {
		t.Fatalf("authentication status = %v, %v", client, err)
	}
	if strings.Contains(err.Error(), "unit_test_only") {
		t.Fatal("authentication error exposed secret response")
	}
}

func TestRevokedTokenIsNotReplayedAndRefreshesOnNextCall(t *testing.T) {
	var logins atomic.Int32
	var revoked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/auth/login" {
			logins.Add(1)
			writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
			return
		}
		if revoked.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writeFixture(t, w, r, `{"code":0,"data":{"hosts":[]}}`)
	}))
	defer server.Close()
	client := openTestClient(t, server.URL)
	revoked.Store(true)
	if _, err := client.GetService(t.Context(), "probe.grpc"); err == nil {
		t.Fatal("revoked authentication was accepted")
	}
	if logins.Load() != 1 {
		t.Fatal("failed operation was transparently replayed")
	}
	revoked.Store(false)
	if _, err := client.GetService(t.Context(), "probe.grpc"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Fatal("next operation did not refresh rejected token")
	}
}

func TestAuthenticatedRedirectIsNotFollowed(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginPath {
			writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
			return
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	t.Setenv("MUSKDEER_UNIT_NACOS_PASSWORD", "unit_test_only")
	client, err := New(t.Context(), testConfig(strings.TrimPrefix(server.URL, "http://")))
	var rejected *APIError
	if client != nil || !errors.As(err, &rejected) || rejected.HTTPStatus != http.StatusFound || redirected.Load() != 0 {
		t.Fatalf("redirect = %v, %v, target requests %d", client, err, redirected.Load())
	}
}

func TestMalformedOrOversizedSnapshotsFailClosed(t *testing.T) {
	for _, body := range []string{`{"code":0,"data":`, strings.Repeat(" ", maxResponseBytes+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == loginPath {
				writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
				return
			}
			writeFixture(t, w, r, body)
		}))
		t.Setenv("MUSKDEER_UNIT_NACOS_PASSWORD", "unit_test_only")
		client, err := New(t.Context(), testConfig(strings.TrimPrefix(server.URL, "http://")))
		server.Close()
		if client != nil || err == nil {
			t.Fatalf("invalid snapshot returned client %v, %v", client, err)
		}
	}
}

func openTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	t.Setenv("MUSKDEER_UNIT_NACOS_PASSWORD", "unit_test_only")
	client, err := New(t.Context(), testConfig(strings.TrimPrefix(serverURL, "http://")))
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

func testConfig(address string) *configv1.Nacos {
	return &configv1.Nacos{
		Address:        address,
		NamespaceId:    "unit-test",
		Group:          "MUSKDEER_TEST",
		Cluster:        "TEST",
		Username:       "nacos",
		PasswordEnv:    "MUSKDEER_UNIT_NACOS_PASSWORD",
		ConnectTimeout: durationpb.New(time.Second),
		CommandTimeout: durationpb.New(time.Second),
		TlsEnabled:     proto.Bool(false),
		MaxInflight:    2,
	}
}

func writeFixture(t *testing.T, w http.ResponseWriter, r *http.Request, value string) {
	t.Helper()
	if _, err := io.WriteString(w, value); err != nil && r.Context().Err() == nil {
		t.Error(err)
	}
}
