package usdm

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const endpoint = "wss://fstream.binance.com/public/ws/"

// Binance sends control pings every three minutes, independently of trades.
const sourceReadWindow = 4 * time.Minute

// Feed owns a single bounded TLS session; its worker owns retry and authority.
type Feed struct {
	symbol            string
	url               string
	dialer            *websocket.Dialer
	snapshotURL       string
	http              *http.Client
	snapshotAdmission *snapshotBudget
}

// NewFeed constructs no network connection or goroutine.
func NewFeed(symbol string) (*Feed, error) {
	if !native.Symbol(symbol) {
		return nil, native.ErrInvalid
	}
	return &Feed{
		symbol:      symbol,
		url:         endpoint + strings.ToLower(symbol) + "@depth20@100ms",
		snapshotURL: "https://fapi.binance.com/fapi/v1/depth?symbol=" + symbol + "&limit=20",
		http: &http.Client{
			Timeout: 500 * time.Millisecond,
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext,
				TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
				TLSHandshakeTimeout: 500 * time.Millisecond,
				MaxConnsPerHost:     1,
				MaxIdleConnsPerHost: 1,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		snapshotAdmission: newSnapshotBudget(),
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
	ctx, cancel := context.WithCancel(parent)
	updates := make(chan *commonv1.VenueBook, 1)
	failures := make(chan error, 1)
	var readers sync.WaitGroup
	readers.Go(func() {
		failures <- f.runFrames(ctx, func(work context.Context, data []byte, received time.Time) error {
			book, err := Decode(data, f.symbol, received)
			if err != nil {
				return err
			}
			select {
			case updates <- book:
			default:
				select {
				case <-updates:
				default:
				}
				select {
				case updates <- book:
				default:
				}
			}
			return work.Err()
		})
	})
	defer func() { cancel(); readers.Wait(); f.http.CloseIdleConnections() }()
	timer := time.NewTimer(snapshotInterval)
	defer timer.Stop()
	var sequence int64
	var cooldown time.Time
	for {
		var book *commonv1.VenueBook
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failures:
			return err
		case book = <-updates:
		case <-timer.C:
			if !time.Now().Before(cooldown) {
				var err error
				book, err = f.snapshot(ctx)
				var limited *snapshotRateLimit
				if errors.As(err, &limited) {
					cooldown = limited.until
				}
			}
			timer.Reset(snapshotInterval)
		}
		if book == nil || book.Sequence < sequence {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		sequence = book.Sequence
		if err := publish(ctx, book); err != nil {
			return err
		}
		timer.Reset(snapshotInterval)
	}
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
	conn.SetPingHandler(func(data string) error {
		if err := conn.SetReadDeadline(time.Now().Add(sourceReadWindow)); err != nil {
			return err
		}
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
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
		if err := conn.SetReadDeadline(time.Now().Add(sourceReadWindow)); err != nil {
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
