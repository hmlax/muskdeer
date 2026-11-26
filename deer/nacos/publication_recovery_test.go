package nacos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

type delayedHeartbeatDeadline struct {
	context.Context
	deadline time.Time
}

func (c delayedHeartbeatDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

type heartbeatReplyTransport struct{}

func (heartbeatReplyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":10200,"clientBeatInterval":50,"lightBeatEnabled":true}`)),
	}, nil
}

func TestPublicationRejectsExpiredReceiptBeforeTimerDelivery(t *testing.T) {
	client, _ := publicationClient(t)
	client.http.Transport = heartbeatReplyTransport{}
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := delayedHeartbeatDeadline{
		Context:  t.Context(),
		deadline: time.Now().Add(-time.Second),
	}
	if _, err := client.Beat(ctx, testRegistration()); err != nil {
		t.Fatalf("fixture did not reproduce a receipt before deadline delivery: %v", err)
	}
	if _, err := publication.renew(ctx, false, time.Now().Add(heartbeatFreshness)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired receipt renewed discovery freshness: %v", err)
	}
}

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

func TestPublicationOutageExpiresReadinessAndRecoversWithoutExit(t *testing.T) {
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
		t.Fatalf("a recoverable outage stopped its process: %v", err)
	case <-time.After(350 * time.Millisecond):
	}
	publication.mu.Lock()
	unchanged := publication.lastSuccess.Equal(lastSuccess)
	publication.mu.Unlock()
	if !unchanged || publication.Health() == nil {
		t.Fatal("failed heartbeats extended discovery freshness")
	}
	if fixture.registrations.Load() != 1 {
		t.Fatal("outage recreated the registration")
	}
	fixture.beatStatus.Store(0)
	awaitPublicationRecovery(t, publication)
	if fixture.registrations.Load() != 1 {
		t.Fatal("a surviving registration was unnecessarily recreated")
	}
}

func TestPublicationRestoresExpiredRegistrationWithOriginalIdentity(t *testing.T) {
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
	publication.lastSuccess = time.Now().Add(-heartbeatFreshness)
	publication.mu.Unlock()
	awaitPublicationBeat(t, publication, fixture)
	fixture.missing.Store(true)
	fixture.beatStatus.Store(0)
	awaitPublicationRecovery(t, publication)
	if fixture.registrations.Load() != 2 {
		t.Fatal("expired registration did not recover exactly once with its original identity")
	}
}

func TestPublicationChecksAmbiguousRestoreBeforePublishingAgain(t *testing.T) {
	client, fixture := publicationClient(t)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.missing.Store(true)
	fixture.registerTimeout.Store(true)
	publication.mu.Lock()
	publication.lastSuccess = time.Now().Add(-heartbeatFreshness)
	publication.mu.Unlock()
	awaitPublicationRecovery(t, publication)
	if fixture.registrations.Load() != 2 {
		t.Fatal("possibly accepted restoration was published again before checking its original identity")
	}
}

func TestStopCancelsAndJoinsRegistrationRecovery(t *testing.T) {
	client, fixture := publicationClient(t)
	publication, err := NewPublication(client, testRegistration(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPublication(t, publication)
	if err := publication.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.hold.Store(true)
	fixture.missing.Store(true)
	publication.mu.Lock()
	publication.lastSuccess = time.Now().Add(-heartbeatFreshness)
	publication.mu.Unlock()
	select {
	case <-fixture.entered:
	case err := <-publication.Failures():
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("registration recovery did not begin")
	}
	if err := publication.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publication.Failures():
		t.Fatalf("normal shutdown reported recovery as a process failure: %v", err)
	default:
	}
	if fixture.registrations.Load() != 2 || fixture.deletions.Load() != 1 {
		t.Fatal("recovery cleanup changed the publication identity or failed to deregister once")
	}
}

func TestHeartbeatRecoveryClassification(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE} {
		wrapped := &APIError{cause: err}
		if !retryHeartbeat(wrapped) {
			t.Errorf("temporary transport failure was terminal: %v", err)
		}
	}
	for _, err := range []error{&APIError{HTTPStatus: http.StatusForbidden}, &APIError{Code: beatMissingCode}, ErrClosed, errors.New("malformed heartbeat")} {
		if retryHeartbeat(err) {
			t.Errorf("permanent or separately handled failure was retried: %v", err)
		}
	}
}

func awaitPublicationRecovery(t *testing.T, publication *Publication) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for publication.Health() != nil {
		select {
		case err := <-publication.Failures():
			t.Fatalf("recovery notified a terminal process failure: %v", err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("publication did not recover after dependency restoration")
		}
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
