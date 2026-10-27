package marketfeed

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/goleak"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const (
	testSnapshot = `{"lastUpdateId":100,"bids":[["99","1"]],"asks":[["101","1"]]}`
	testBridge   = `{"e":"depthUpdate","E":1500,"s":"BTCUSDT","U":101,"u":101,"b":[["99","2"]],"a":[]}`
	testGap      = `{"e":"depthUpdate","E":1501,"s":"BTCUSDT","U":103,"u":103,"b":[],"a":[]}`
	testOKX      = `{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"snapshot","data":[{"bids":[["99","1","0","1"]],"asks":[["101","1","0","1"]],"ts":"1500","prevSeqId":-1,"seqId":100,"checksum":0}]}`
)

func TestBinanceBuffersBeforeSnapshotAndRebuildsAfterGap(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var attempts atomic.Int64
	var snapshots atomic.Int64
	wsStarted := make(chan struct{}, 2)
	allowGap := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/depth" {
			select {
			case <-wsStarted:
			case <-ctx.Done():
				return
			}
			if r.URL.Query().Get("symbol") != "BTCUSDT" || r.URL.Query().Get("limit") != "1000" {
				t.Error("snapshot request did not preserve its explicit symbol/depth")
			}
			snapshots.Add(1)
			_, _ = w.Write([]byte(testSnapshot))
			return
		}
		if r.URL.Path != "/ws/btcusdt@depth@100ms" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		attempt := attempts.Add(1)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testBridge))
		select {
		case wsStarted <- struct{}{}:
		case <-ctx.Done():
			return
		}
		if attempt == 1 {
			select {
			case <-allowGap:
			case <-ctx.Done():
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(testGap))
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceBinance)
	var observations []Observation
	var firstConnection int64
	err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
		observations = append(observations, observation)
		if observation.State != marketbook.StateReady {
			if !observation.View.Mid.IsZero() {
				t.Error("invalid observation exposed old prices")
			}
			return nil
		}
		if observation.View.Sequence != 101 || observation.View.Mid.String() != "100" || observation.Source != domain.MarketSourceBinance {
			t.Errorf("incorrect bridged book: %+v", observation)
		}
		if firstConnection == 0 {
			firstConnection = observation.Connection
			close(allowGap)
			return nil
		}
		if observation.Connection <= firstConnection {
			t.Error("recovered book reused connection identity")
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || snapshots.Load() != 2 || attempts.Load() != 2 {
		t.Fatalf("recovery = %v, snapshots=%d, sessions=%d", err, snapshots.Load(), attempts.Load())
	}
	invalid := false
	for _, observation := range observations {
		if observation.State == marketbook.StateInvalid && observation.Reason == ReasonSequence {
			invalid = true
		}
	}
	if !invalid {
		t.Fatal("sequence gap was never invalidated before recovery")
	}
}

func TestOKXSubscriptionHeartbeatAndControlNeverRefreshPrices(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	var pings atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var subscription subscriptionRequest
		if err := conn.ReadJSON(&subscription); err != nil || subscription.Operation != marketwire.OKXOpSubscribe || len(subscription.Arguments) != 1 || subscription.Arguments[0].Symbol != "BTC-USDT" || subscription.Arguments[0].Channel != marketwire.OKXChannelBooks {
			t.Errorf("incorrect typed subscription: %+v, %v", subscription, err)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"books","instId":"BTC-USDT"},"connId":"connection"}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testOKX))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(data) != marketwire.OKXPing {
				t.Error("unexpected application heartbeat")
				return
			}
			pings.Add(1)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(marketwire.OKXPong))
			if pings.Load() == 2 {
				cancel()
			}
		}
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceOKX)
	feed.config.PingInterval = 30 * time.Millisecond
	var ready int
	err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
		if observation.State == marketbook.StateReady {
			ready++
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || ready != 1 || pings.Load() != 2 {
		t.Fatalf("heartbeat refreshed book or failed: ready=%d, ping=%d, err=%v", ready, pings.Load(), err)
	}
}

func TestSnapshotCancellationClosesSocketAndReturnsCause(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	requestStarted := make(chan struct{})
	socketClosed := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/depth" {
			close(requestStarted)
			<-r.Context().Done()
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testBridge))
		_, _, _ = conn.ReadMessage()
		close(socketClosed)
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceBinance)
	finished := make(chan error, 1)
	go func() {
		finished <- feed.Run(ctx, func(context.Context, Observation) error { return nil })
	}()
	select {
	case <-requestStarted:
		cancel()
	case <-ctx.Done():
		t.Fatal("REST snapshot never started")
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	select {
	case <-socketClosed:
	case <-time.After(time.Second):
		t.Fatal("socket reader survived Run")
	}
}

func TestSlowSnapshotBufferOverflowInvalidatesAndObserverFailureStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	requestStarted := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/depth" {
			close(requestStarted)
			<-r.Context().Done()
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testBridge))
		select {
		case <-requestStarted:
		case <-ctx.Done():
			return
		}
		for range 4 {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(testBridge))
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceBinance)
	feed.config.BufferMessages = 1
	failure := errors.New("sink failed with private details")
	err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
		if observation.State == marketbook.StateReady {
			t.Error("overflow authorized a price")
		}
		if observation.State == marketbook.StateInvalid {
			if observation.Reason != ReasonOverflow {
				t.Errorf("wrong failure classification: %s", observation.Reason)
			}
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || strings.Contains(err.Error(), "private") {
		t.Fatalf("sink failure retried or exposed raw details: %v", err)
	}
}

func TestConfigurationBindsOnlyPublicSupportedSources(t *testing.T) {
	for _, source := range []domain.MarketSource{domain.MarketSourceBitget, domain.MarketSourceBybit, "other"} {
		if _, err := New(DefaultConfig(source, marketwire.Instrument{
			ID:     1,
			Symbol: "BTCUSDT",
		})); err == nil {
			t.Fatal("candidate or unknown source was enabled")
		}
	}
	config := DefaultConfig(domain.MarketSourceBinance, marketwire.Instrument{
		ID:     1,
		Symbol: "BTCUSDT?other=1",
	})
	if _, err := New(config); err == nil {
		t.Fatal("unsafe symbol accepted")
	}
}

func TestOKXGapInvalidatesAndReconnectsWithoutChangingSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	var attempts atomic.Int64
	allowGap := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _, _ = conn.ReadMessage()
		attempt := attempts.Add(1)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"books","instId":"BTC-USDT"},"connId":"connection"}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testOKX))
		if attempt == 1 {
			select {
			case <-allowGap:
			case <-ctx.Done():
				return
			}
			gap := strings.Replace(testOKX, `"action":"snapshot"`, `"action":"update"`, 1)
			gap = strings.Replace(gap, `"prevSeqId":-1,"seqId":100`, `"prevSeqId":101,"seqId":102`, 1)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(gap))
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceOKX)
	var ready int
	invalid := false
	err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
		if observation.Source != domain.MarketSourceOKX || observation.Symbol != "BTC-USDT" {
			t.Error("recovery changed source identity")
		}
		if observation.State == marketbook.StateInvalid {
			invalid = observation.Reason == ReasonSequence && observation.View.Mid.IsZero()
		}
		if observation.State == marketbook.StateReady {
			ready++
			if ready == 1 {
				close(allowGap)
			} else {
				cancel()
			}
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || ready != 2 || attempts.Load() != 2 || !invalid {
		t.Fatalf("OKX recovery: ready=%d, connections=%d, invalid=%t, err=%v", ready, attempts.Load(), invalid, err)
	}
}

func TestBinancePingPongCopiesExactPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	const payload = "provider-ping-payload"
	var pong atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		conn.SetPongHandler(func(reply string) error {
			pong.Store(reply == payload)
			cancel()
			return nil
		})
		_ = conn.WriteControl(websocket.PingMessage, []byte(payload), time.Now().Add(time.Second))
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceBinance)
	err := feed.Run(ctx, func(context.Context, Observation) error { return nil })
	if !errors.Is(err, context.Canceled) || !pong.Load() {
		t.Fatalf("ping payload was not echoed: %v", err)
	}
}

func TestBinanceLaggingSnapshotAndCoveredMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	var snapshots atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/depth" {
			body := testSnapshot
			if snapshots.Add(1) == 1 {
				body = strings.Replace(body, `"lastUpdateId":100`, `"lastUpdateId":90`, 1)
			}
			_, _ = w.Write([]byte(body))
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.Replace(testBridge, `"U":101,"u":101`, `"U":100,"u":100`, 1)))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(testBridge))
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceBinance)
	var ready int
	err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
		if observation.State == marketbook.StateReady {
			ready++
			if observation.View.Sequence != 101 {
				t.Error("covered update was emitted as a fresh price")
			}
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || ready != 1 || snapshots.Load() != 2 {
		t.Fatalf("lagging snapshot: ready=%d, snapshots=%d, err=%v", ready, snapshots.Load(), err)
	}
}

func TestSocketRejectsOversizedAndBinaryMessages(t *testing.T) {
	for _, kind := range []int{websocket.TextMessage, websocket.BinaryMessage} {
		name := "oversized"
		if kind == websocket.BinaryMessage {
			name = "binary"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			upgrader := websocket.Upgrader{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				_, _, _ = conn.ReadMessage()
				data := []byte(testOKX)
				if kind == websocket.TextMessage {
					data = []byte(strings.Repeat(" ", MaximumBytes+1))
				}
				_ = conn.WriteMessage(kind, data)
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			feed := localFeed(t, server, domain.MarketSourceOKX)
			failure := errors.New("observed rejection")
			err := feed.Run(ctx, func(_ context.Context, observation Observation) error {
				if observation.State == marketbook.StateReady {
					t.Fatal("invalid frame produced a price")
				}
				if observation.State == marketbook.StateInvalid {
					return failure
				}
				return nil
			})
			if !errors.Is(err, failure) {
				t.Fatalf("frame rejection not delivered: %v", err)
			}
		})
	}
}

func TestRESTDoesNotFollowRedirectOrAcceptUntrustedTLS(t *testing.T) {
	var followed atomic.Int64
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		_, _ = w.Write([]byte(testSnapshot))
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	feed := localFeed(t, redirect, domain.MarketSourceBinance)
	if _, err := feed.snapshot(t.Context()); err == nil || followed.Load() != 0 {
		t.Fatalf("redirect was followed: %v", err)
	}
	feed.transport.CloseIdleConnections()
	feed.transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if _, err := feed.snapshot(t.Context()); err == nil {
		t.Fatal("untrusted TLS certificate was accepted")
	}
	feed.transport.CloseIdleConnections()
}

func TestConcurrentRunRejectedAndTasksJoined(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not a websocket", http.StatusBadRequest)
	}))
	defer server.Close()
	feed := localFeed(t, server, domain.MarketSourceOKX)
	started := make(chan struct{})
	var once sync.Once
	finished := make(chan error, 1)
	go func() {
		finished <- feed.Run(ctx, func(context.Context, Observation) error {
			once.Do(func() { close(started) })
			return nil
		})
	}()
	<-started
	if err := feed.Run(ctx, func(context.Context, Observation) error { return nil }); !errors.Is(err, ErrRunning) {
		t.Errorf("concurrent Run accepted: %v", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("owner did not join: %v", err)
	}
}

func localFeed(t *testing.T, server *httptest.Server, source domain.MarketSource) *Feed {
	t.Helper()
	symbol := "BTCUSDT"
	if source == domain.MarketSourceOKX {
		symbol = "BTC-USDT"
	}
	feed, err := New(DefaultConfig(source, marketwire.Instrument{
		ID:     1,
		Symbol: symbol,
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig.MinVersion = tls.VersionTLS12
	feed.transport.CloseIdleConnections()
	feed.transport = transport
	feed.http.Transport = transport
	feed.dialer.TLSClientConfig = transport.TLSClientConfig.Clone()
	feed.restURL = server.URL + "/api/v3/depth"
	feed.wsURL = "wss" + strings.TrimPrefix(server.URL, "https")
	if source == domain.MarketSourceBinance {
		feed.wsURL += "/ws/btcusdt@depth@100ms"
	}
	feed.config.RetryDelay = 10 * time.Millisecond
	return feed
}
