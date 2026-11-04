package usdm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	markethimalayav1 "github.com/hmlax/muskdeer/deer/proto/market/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAggregateTradeFrameRetainsExactValuesAndRejectsInvalidSource(t *testing.T) {
	t.Parallel()
	now := time.Now()
	maker := true
	frame := tradeFrame{
		Event:      aggregateTradeEvent,
		EventAt:    now.UnixMilli(),
		TradeAt:    now.UnixMilli(),
		Symbol:     "BTCUSDT",
		ID:         42,
		Price:      "100.123456789123456789",
		Quantity:   "0.123456789123456789",
		First:      1,
		Last:       3,
		BuyerMaker: &maker,
	}
	encode := func(value tradeFrame) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	trade, err := DecodeTrade(encode(frame), "BTCUSDT", now)
	if err != nil || trade.GetSide() != string(domain.SideSell) || trade.GetPrice() != frame.Price || trade.GetQuantity() != frame.Quantity || trade.GetEventId() != "42" {
		t.Fatalf("DecodeTrade(buyer maker) = %v, %v, want exact aggressive sell", trade, err)
	}
	maker = false
	trade, err = DecodeTrade(encode(frame), "BTCUSDT", now)
	if err != nil || trade.GetSide() != string(domain.SideBuy) {
		t.Fatalf("DecodeTrade(taker buy) = %v, %v", trade, err)
	}
	for _, change := range []func(*tradeFrame){
		func(f *tradeFrame) { f.BuyerMaker = nil },
		func(f *tradeFrame) { f.Symbol = "ETHUSDT" },
		func(f *tradeFrame) { f.ID = 0 },
		func(f *tradeFrame) { f.Event = "trade" },
		func(f *tradeFrame) { f.Price = "NaN" },
		func(f *tradeFrame) { f.Quantity = "0" },
		func(f *tradeFrame) { f.EventAt = now.Add(time.Second).UnixMilli() },
		func(f *tradeFrame) { f.TradeAt = now.Add(-2 * time.Second).UnixMilli() },
		func(f *tradeFrame) { f.First = 4 },
		func(f *tradeFrame) { kind := int32(2); f.SymbolType = &kind },
	} {
		invalid := frame
		change(&invalid)
		if _, err := DecodeTrade(encode(invalid), "BTCUSDT", now); err == nil {
			t.Fatal("DecodeTrade(invalid aggregate observation) succeeded")
		}
	}
}

func TestAggregateTradeReadRequiresAnAuthenticatedService(t *testing.T) {
	runtime := &Runtime{}
	if _, err := runtime.GetTrade(t.Context(), &markethimalayav1.TradeRequest{Symbol: "BTCUSDT"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetTrade(without peer identity)=%v, want PermissionDenied", err)
	}
}

func TestAggregateSessionRejectsRepeatedIDsAndJoinsItsSocket(t *testing.T) {
	testAggregateSession(t, false)
}

func TestAggregateCancellationDropsBufferedFramesAndJoinsItsSocket(t *testing.T) {
	testAggregateSession(t, true)
}

func testAggregateSession(t *testing.T, cancelAfterFirst bool) {
	t.Helper()
	limit, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(limit)
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
		stamp := time.Now().UnixMilli()
		frame := tradeFrame{
			Event:      aggregateTradeEvent,
			EventAt:    stamp,
			TradeAt:    stamp,
			Symbol:     "BTCUSDT",
			ID:         42,
			Price:      "100.4",
			Quantity:   "1",
			First:      1,
			Last:       1,
			BuyerMaker: new(bool),
		}
		for index := range 2 {
			if cancelAfterFirst {
				frame.ID = 42 + int64(index)
			}
			data, err := json.Marshal(frame)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	feed, err := NewTradeFeed("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(feed.session.url, aggregateTradeEndpoint) {
		t.Fatal("aggregate stream used the depth route")
	}
	feed.session.url = "wss" + strings.TrimPrefix(server.URL, "https")
	feed.session.dialer.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	received := 0
	err = feed.Run(ctx, func(context.Context, *commonv1.HimalayaTrade) error {
		received++
		if cancelAfterFirst {
			cancel()
		}
		return nil
	})
	want := native.ErrInvalid
	if cancelAfterFirst {
		want = context.Canceled
	}
	if !errors.Is(err, want) || received != 1 {
		t.Fatalf("aggregate repeat ID: received=%d err=%v, want one delivery then rejection", received, err)
	}
	select {
	case <-finished:
	case <-limit.Done():
		t.Fatal("aggregate socket handler was not joined")
	}
}
