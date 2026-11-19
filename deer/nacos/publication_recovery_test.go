package nacos

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPublicationRecoversAfterOneHeartbeatTimeout(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.beatTimeout.Store(true)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		awaitPublicationBeat(t, publication, fixture)
	}
	if err := publication.Health(); err != nil {
		t.Fatalf("recovered publication unhealthy: %v", err)
	}
	if fixture.registrations.Load() != 1 {
		t.Fatal("timeout recovery re-registered the instance")
	}
}

func TestPublicationRecoversAfterTemporaryHTTPFailure(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.beatStatus.Store(http.StatusServiceUnavailable)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitPublicationBeat(t, publication, fixture)
	fixture.beatStatus.Store(0)
	for range 2 {
		awaitPublicationBeat(t, publication, fixture)
	}
	if err := publication.Health(); err != nil {
		t.Fatalf("temporary failure stopped publication: %v", err)
	}
}

func TestPublicationOutageCannotExtendLastSuccessfulHeartbeat(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.beatStatus.Store(http.StatusServiceUnavailable)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	publication.mu.Lock()
	lastSuccess := time.Now().Add(-2*maximumBeat + 200*time.Millisecond)
	publication.lastSuccess = lastSuccess
	publication.mu.Unlock()
	select {
	case err := <-publication.Failures():
		if err == nil || time.Since(lastSuccess) < 2*maximumBeat || publication.Health() == nil {
			t.Fatalf("publication stopped before freshness expired or stayed healthy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("continued failures extended publication freshness")
	}
	if fixture.registrations.Load() != 1 {
		t.Fatal("outage recreated the registration")
	}
}

func TestPublicationAuthenticationFailureRemainsTerminal(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.beatStatus.Store(http.StatusForbidden)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publication.Failures():
		var failure *APIError
		if !errors.As(err, &failure) || failure.HTTPStatus != http.StatusForbidden || publication.Health() == nil {
			t.Fatalf("authentication failure = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("authentication failure was retried as a transient outage")
	}
}

func TestStopJoinsHeartbeatRecoveryWithoutFailureNotification(t *testing.T) {
	client, fixture := publicationClient(t)
	fixture.beatTimeout.Store(true)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitPublicationBeat(t, publication, fixture)
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publication.Failures():
		t.Fatalf("normal stop reported a heartbeat failure: %v", err)
	default:
	}
	if fixture.deletions.Load() != 1 {
		t.Fatal("stop did not deregister once")
	}
}

func awaitPublicationBeat(t *testing.T, publication *Publication, fixture *publicationFixture) {
	t.Helper()
	select {
	case <-fixture.beats:
	case err := <-publication.Failures():
		t.Fatalf("temporary heartbeat failure stopped publication: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not resume heartbeats")
	}
}
