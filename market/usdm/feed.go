package usdm

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const endpoint = "wss://fstream.binance.com/public/ws/"

// Feed owns a single bounded TLS session; its worker owns retry and authority.
type Feed struct {
	symbol string
	url    string
	dialer *websocket.Dialer
}

// NewFeed constructs no network connection or goroutine.
func NewFeed(symbol string) (*Feed, error) {
	if !native.Symbol(symbol) {
		return nil, native.ErrInvalid
	}
	return &Feed{
		symbol: symbol,
		url:    endpoint + strings.ToLower(symbol) + "@depth20@100ms",
		dialer: &websocket.Dialer{
			Proxy:            nil,
			NetDialContext:   (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
			HandshakeTimeout: 5 * time.Second,
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
		},
	}, nil
}

// Run returns after its socket and cancellation callback are joined. Each frame
// is a complete top-twenty observation. Stale/reversed sequence is rejected;
// consumers receive no cached data after a session exits.
func (f *Feed) Run(parent context.Context, publish func(context.Context, *commonv1.VenueBook) error) (result error) {
	if parent == nil || publish == nil {
		return native.ErrInvalid
	}
	var sequence int64
	return f.runFrames(parent, func(ctx context.Context, data []byte, received time.Time) error {
		book, err := Decode(data, f.symbol, received)
		if err != nil || book.Sequence <= sequence {
			return native.ErrInvalid
		}
		sequence = book.Sequence
		return publish(ctx, book)
	})
}

func (f *Feed) runFrames(parent context.Context, publish func(context.Context, []byte, time.Time) error) (result error) {
	ctx, cancel := context.WithTimeout(parent, 23*time.Hour)
	defer cancel()
	conn, response, err := f.dialer.DialContext(ctx, f.url, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return err
	}
	conn.SetReadLimit(64 * 1024)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(closed) })
	defer func() {
		result = errors.Join(result, conn.Close())
		if !stop() {
			<-closed
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return errors.Join(ctx.Err(), err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if kind != websocket.TextMessage {
			return native.ErrInvalid
		}
		if err := publish(ctx, data, time.Now()); err != nil {
			return err
		}
	}
}
