package usdm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/goleak"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestDepthSessionCancellationClosesAndJoinsTheSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	finished := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		data := fmt.Sprintf(`{"e":"depthUpdate","E":%d,"s":"BTCUSDT","U":1,"u":2,"b":[["99","1"]],"a":[["101","1"]]}`, time.Now().UnixMilli())
		if err := conn.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
			return
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed, err := NewFeed("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	feed.url = "wss" + strings.TrimPrefix(server.URL, "https")
	feed.dialer.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	received := 0
	err = feed.Run(ctx, func(context.Context, *commonv1.HimalayaBook) error {
		received++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || received != 1 {
		t.Fatalf("owned session did not stop: received=%d err=%v", received, err)
	}
	select {
	case <-finished:
	case <-t.Context().Done():
		t.Fatal("server did not observe socket closure")
	}
}
