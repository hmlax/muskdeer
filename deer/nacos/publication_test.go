package nacos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type publicationFixture struct {
	registrations atomic.Int32
	deletions     atomic.Int32
	missing       atomic.Bool
	reject        atomic.Bool
	hold          atomic.Bool
	entered       chan struct{}
	beats         chan struct{}
}

func TestPublicationStopsHeartbeatsAndDeregistersOnce(t *testing.T) {
	client, fixture := publicationClient(t)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	lost := publication.Failures()
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-fixture.beats:
		case err := <-lost:
			t.Fatal(err)
		case <-time.After(time.Second):
			t.Fatal("publication did not maintain heartbeats")
		}
	}
	if err := publication.Health(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := publication.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if fixture.registrations.Load() != 1 || fixture.deletions.Load() != 1 || !errors.Is(publication.Health(), ErrClosed) {
		t.Fatalf("publication ownership = register %d, delete %d, health %v", fixture.registrations.Load(), fixture.deletions.Load(), publication.Health())
	}
}

func TestLostPublicationFailsWithoutReregistering(t *testing.T) {
	client, fixture := publicationClient(t)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	lost := publication.Failures()
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.missing.Store(true)
	select {
	case err := <-lost:
		if err == nil || publication.Health() == nil {
			t.Fatal("lost registration still reported healthy")
		}
	case <-time.After(time.Second):
		t.Fatal("lost registration did not notify its owner")
	}
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.registrations.Load() != 1 {
		t.Fatal("heartbeat recreated a missing registration")
	}
}

func TestAmbiguousRegistrationStillHasOwnedCleanup(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.reject.Store(true)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err == nil {
		t.Fatal("failed registration reported successful startup")
	}
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.registrations.Load() != 1 || fixture.deletions.Load() != 1 {
		t.Fatal("possibly accepted registration was not cleaned up")
	}
}

func TestStopCancelsRegistrationAndJoinsStartup(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.hold.Store(true)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	started := make(chan error, 1)
	go func() { started <- publication.Start(t.Context()) }()
	select {
	case <-fixture.entered:
	case <-time.After(time.Second):
		t.Fatal("registration did not enter the server")
	}
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted registration = %v", err)
	}
	if fixture.deletions.Load() != 1 {
		t.Fatal("interrupted registration was not deregistered")
	}
}

func TestUnusedPublicationStopsWithoutNetwork(t *testing.T) {
	client, fixture := publicationClient(t)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.registrations.Load() != 0 || fixture.deletions.Load() != 0 {
		t.Fatal("unused publication changed discovery")
	}
	if err := publication.Start(t.Context()); err == nil {
		t.Fatal("closed publication restarted")
	}
}

func cleanupPublication(t *testing.T, publication *Publication) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := publication.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
}

func publicationClient(t *testing.T) (*Client, *publicationFixture) {
	t.Helper()
	fixture := &publicationFixture{
		beats:   make(chan struct{}, 10),
		entered: make(chan struct{}),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		switch r.URL.Path {
		case loginPath:
			writeFixture(t, w, r, `{"accessToken":"unit_token_only","tokenTtl":18000}`)
		case instanceListPath:
			writeFixture(t, w, r, `{"code":0,"data":{"hosts":[]}}`)
		case "/nacos/v2/ns/instance":
			if r.Method == http.MethodDelete {
				fixture.deletions.Add(1)
				writeFixture(t, w, r, `{"code":0,"data":"ok"}`)
				return
			}
			fixture.registrations.Add(1)
			if fixture.hold.Load() {
				close(fixture.entered)
				<-r.Context().Done()
				return
			}
			var metadata instanceMetadata
			if err := json.Unmarshal([]byte(r.Form.Get("metadata")), &metadata); err != nil || metadata.ID != testRegistration().ID || metadata.Kind != "grpc" {
				t.Error("registration lost typed identity metadata")
			}
			if fixture.reject.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeFixture(t, w, r, `{"code":0,"data":"ok"}`)
		case "/nacos/v1/ns/instance/beat":
			if r.Form.Has("beat") || r.Form.Get("serviceName") != "MUSKDEER_TEST@@probe.grpc" || r.Form.Get("port") != "8081" {
				t.Error("heartbeat must be light and scoped to its registered endpoint")
			}
			select {
			case fixture.beats <- struct{}{}:
			default:
			}
			if fixture.missing.Load() {
				writeFixture(t, w, r, `{"code":20404,"clientBeatInterval":50,"lightBeatEnabled":true}`)
				return
			}
			writeFixture(t, w, r, `{"code":10200,"clientBeatInterval":50,"lightBeatEnabled":true}`)
		default:
			t.Errorf("unexpected naming request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client := openTestClient(t, strings.TrimSuffix(server.URL, "/"))
	return client, fixture
}

func testRegistration() Registration {
	return Registration{
		ID:      "node-a",
		Service: "probe.grpc",
		Version: "test",
		IP:      "127.0.0.1",
		Port:    8081,
	}
}
