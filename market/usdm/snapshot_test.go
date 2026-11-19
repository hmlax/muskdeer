package usdm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestQuietDepthUsesActualSnapshotsWithoutRewritingExchangeTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	feed, calls, finished := quietDepthFixture(t, false)
	var observations int
	err := feed.Run(ctx, func(_ context.Context, book *common.VenueBook) error {
		if book.SnapshotStartedAtMs == 0 {
			return nil
		}
		if book.EventAtMs >= book.SnapshotStartedAtMs-9000 || book.Sequence != 10 || book.ValidUntilMs > book.SnapshotStartedAtMs+native.BookAge.Milliseconds() || book.ValidUntilMs <= time.Now().UnixMilli() {
			t.Errorf("snapshot changed event identity or extended observation lifetime: %v", book)
		}
		observations++
		if observations == 4 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || observations != 4 || calls.Load() != 4 {
		t.Fatalf("quiet feed snapshots=%d calls=%d err=%v", observations, calls.Load(), err)
	}
	select {
	case <-finished:
	case <-t.Context().Done():
		t.Fatal("socket was not joined")
	}
}

func TestFailedSnapshotCannotRenewAnOldBook(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2400*time.Millisecond)
	defer cancel()
	feed, calls, _ := quietDepthFixture(t, true)
	var latest *common.VenueBook
	err := feed.Run(ctx, func(_ context.Context, book *common.VenueBook) error {
		latest = proto.CloneOf(book)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() < 2 || latest == nil || latest.ValidUntilMs > time.Now().UnixMilli() || latest.SnapshotStartedAtMs != 0 {
		t.Fatalf("failed refresh manufactured freshness: calls=%d latest=%v err=%v", calls.Load(), latest, err)
	}
}

func quietDepthFixture(t *testing.T, fail bool) (*Feed, *atomic.Int64, <-chan struct{}) {
	t.Helper()
	var calls atomic.Int64
	finished := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snapshot" {
			calls.Add(1)
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if _, err := fmt.Fprintf(w, `{"lastUpdateId":10,"E":%d,"T":%d,"bids":[["99","1"]],"asks":[["101","1"]]}`, time.Now().Add(-10*time.Second).UnixMilli(), time.Now().Add(-10*time.Second).UnixMilli()); err != nil {
				return
			}
			return
		}
		defer close(finished)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		frame := fmt.Sprintf(`{"e":"depthUpdate","E":%d,"s":"SUNUSDT","U":10,"u":10,"b":[["99","1"]],"a":[["101","1"]]}`, time.Now().UnixMilli())
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			return
		}
		_, _, _ = conn.ReadMessage()
	}))
	t.Cleanup(server.Close)
	feed, err := NewFeed("SUNUSDT")
	if err != nil {
		t.Fatal(err)
	}
	feed.url = "wss" + strings.TrimPrefix(server.URL, "https")
	feed.dialer.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	feed.http = server.Client()
	feed.snapshotURL = server.URL + "/snapshot"
	return feed, &calls, finished
}

func TestSnapshotRateLimitBackoffIsSharedAcrossSymbols(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	first, err := NewFeed("SUNUSDT")
	if err != nil {
		t.Fatal(err)
	}
	first.snapshotURL = server.URL
	_, err = first.snapshot(t.Context())
	var limited *snapshotRateLimit
	if !errors.As(err, &limited) {
		t.Fatalf("rate limit=%v", err)
	}
	second, err := NewFeed("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	second.snapshotURL = server.URL
	second.snapshotAdmission = first.snapshotAdmission
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if book, err := second.snapshot(ctx); book != nil || !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("shared source backoff book=%v err=%v requests=%d", book, err, calls.Load())
	}
}
