// Package marketfeed owns bounded public TLS market sessions. Run owns and
// joins all socket tasks; construction starts no work and needs no credentials.
package marketfeed

import (
	"context"
	"crypto/tls"
	"errors"
	"math"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

// Reason is safe to persist or log; raw provider bodies/URLs are never errors.
type Reason string

// Session classifications, payload bounds and fixed safe failure messages.
const (
	ReasonNone      Reason = ""
	ReasonTransport Reason = "TRANSPORT"
	ReasonSequence  Reason = "SEQUENCE"
	ReasonPayload   Reason = "PAYLOAD"
	ReasonOverflow  Reason = "OVERFLOW"
	MaximumBytes           = 1024 * 1024
	MaximumBuffer          = 8 * MaximumBytes
	MessageConfig          = "market feed configuration is invalid"
	MessageSession         = "market feed session requires recovery"
	MessageOverflow        = "market feed buffer capacity was exceeded"
	MessageObserver        = "market feed observer failed"
	MessageRunning         = "market feed is already running"
	binanceREST            = "https://data-api.binance.vision/api/v3/depth"
	binanceWS              = "wss://data-stream.binance.vision:443/ws/"
	okxWS                  = "wss://ws.okx.com:8443/ws/v5/public"
)

// Errors preserve causes without printing raw network details or payloads.
var (
	ErrConfig   = errors.New(MessageConfig)
	ErrSession  = errors.New(MessageSession)
	ErrOverflow = errors.New(MessageOverflow)
	ErrObserver = errors.New(MessageObserver)
	ErrRunning  = errors.New(MessageRunning)
)

// Config has explicit finite bounds. Endpoints are fixed public market hosts;
// production callers cannot inject a proxy, redirect, private URL or credential.
type Config struct {
	Source           domain.MarketSource
	Instrument       marketwire.Instrument
	SnapshotDepth    int
	HandshakeTimeout time.Duration
	RequestTimeout   time.Duration
	ReadTimeout      time.Duration
	SessionLifetime  time.Duration
	PingInterval     time.Duration
	RetryDelay       time.Duration
	BufferMessages   int
	BufferBytes      int64
}

// Observation carries prices only in READY. Connection is process-local and
// cannot authorize distributed publication or execution.
type Observation struct {
	Source       domain.MarketSource
	InstrumentID int64
	Symbol       string
	Connection   int64
	State        marketbook.State
	Reason       Reason
	View         marketbook.View
}

// Observer must honor its bounded context. Failure stops the feed; only network
// or provider protocol failures reconnect. Calls are serial, never concurrent.
type Observer func(context.Context, Observation) error

// Feed is a single active Run owner with private HTTP and WebSocket transports.
type Feed struct {
	config    Config
	restURL   string
	wsURL     string
	transport *http.Transport
	http      *http.Client
	dialer    *websocket.Dialer
	running   atomic.Bool
}

type safeError struct {
	class error
	cause error
}

func (e *safeError) Error() string { return e.class.Error() }
func (e *safeError) Unwrap() error { return errors.Join(e.class, e.cause) }

// DefaultConfig uses Binance's official market-only hosts and OKX public books.
func DefaultConfig(source domain.MarketSource, instrument marketwire.Instrument) Config {
	return Config{
		Source:           source,
		Instrument:       instrument,
		SnapshotDepth:    1000,
		HandshakeTimeout: 5 * time.Second,
		RequestTimeout:   5 * time.Second,
		ReadTimeout:      45 * time.Second,
		SessionLifetime:  23*time.Hour + 50*time.Minute,
		PingInterval:     20 * time.Second,
		RetryDelay:       2 * time.Second,
		BufferMessages:   256,
		BufferBytes:      MaximumBuffer,
	}
}

// New validates a supported source and constructs no connection or goroutine.
func New(config Config) (*Feed, error) {
	if err := validate(config); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: config.HandshakeTimeout}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    config.HandshakeTimeout,
		ResponseHeaderTimeout:  config.RequestTimeout,
		MaxResponseHeaderBytes: 16 * 1024,
		MaxConnsPerHost:        1,
		MaxIdleConnsPerHost:    1,
		IdleConnTimeout:        30 * time.Second,
	}
	feed := &Feed{
		config:    config,
		transport: transport,
		http: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		dialer: &websocket.Dialer{
			Proxy:            nil,
			NetDialContext:   (&net.Dialer{Timeout: config.HandshakeTimeout}).DialContext,
			TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
			HandshakeTimeout: config.HandshakeTimeout,
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
		},
	}
	feed.wsURL = okxWS
	if config.Source == domain.MarketSourceBinance {
		feed.restURL = binanceREST
		feed.wsURL = binanceWS + strings.ToLower(config.Instrument.Symbol) + "@depth@100ms"
	}
	return feed, nil
}

// Run publishes invalidation before reconnecting to the same provider. It
// returns only after its reader/cancellation work and HTTP requests have joined.
func (f *Feed) Run(ctx context.Context, observer Observer) error {
	if ctx == nil || observer == nil {
		return ErrConfig
	}
	if !f.running.CompareAndSwap(false, true) {
		return ErrRunning
	}
	defer f.running.Store(false)
	defer f.transport.CloseIdleConnections()
	for connection := int64(1); ; connection++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if connection == math.MaxInt64 {
			return ErrSession
		}
		if err := f.notify(ctx, observer, connection, marketbook.StateSynchronizing, ReasonNone, marketbook.View{}); err != nil {
			return err
		}
		err := f.session(ctx, observer, connection)
		if errors.Is(err, ErrObserver) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := f.notify(ctx, observer, connection, marketbook.StateInvalid, classify(err), marketbook.View{}); err != nil {
			return err
		}
		if err := wait(ctx, f.config.RetryDelay); err != nil {
			return err
		}
	}
}

func (f *Feed) notify(ctx context.Context, observer Observer, connection int64, state marketbook.State, reason Reason, view marketbook.View) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, f.config.RequestTimeout)
	defer cancel()
	err := observer(call, Observation{
		Source:       f.config.Source,
		InstrumentID: f.config.Instrument.ID,
		Symbol:       f.config.Instrument.Symbol,
		Connection:   connection,
		State:        state,
		Reason:       reason,
		View:         view,
	})
	if err == nil {
		err = call.Err()
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if err != nil {
		return &safeError{
			class: ErrObserver,
			cause: err,
		}
	}
	return nil
}

func validate(config Config) error {
	if _, err := marketbook.New(config.Source, config.Instrument.ID, config.Instrument.Symbol, marketbook.MaximumLevels); err != nil {
		return ErrConfig
	}
	if config.SnapshotDepth < 1 || config.SnapshotDepth > marketbook.MaximumLevels ||
		config.HandshakeTimeout < 100*time.Millisecond || config.HandshakeTimeout > 30*time.Second ||
		config.RequestTimeout < 100*time.Millisecond || config.RequestTimeout > 30*time.Second ||
		config.PingInterval < 100*time.Millisecond || config.PingInterval > 20*time.Second ||
		config.ReadTimeout < 2*config.PingInterval || config.ReadTimeout > 2*time.Minute ||
		config.SessionLifetime < time.Second || config.SessionLifetime > 23*time.Hour+50*time.Minute ||
		config.RetryDelay < time.Second || config.RetryDelay > time.Minute ||
		config.BufferMessages < 1 || config.BufferMessages > 512 ||
		config.BufferBytes < 1024 || config.BufferBytes > MaximumBuffer {
		return ErrConfig
	}
	if config.Source == domain.MarketSourceBinance && (strings.ToUpper(config.Instrument.Symbol) != config.Instrument.Symbol || strings.ContainsAny(config.Instrument.Symbol, "_-")) {
		return ErrConfig
	}
	return nil
}

func classify(err error) Reason {
	switch {
	case errors.Is(err, ErrOverflow):
		return ReasonOverflow
	case errors.Is(err, marketbook.ErrSequenceGap), errors.Is(err, marketbook.ErrNotReady):
		return ReasonSequence
	case errors.Is(err, marketwire.ErrInvalidPayload), errors.Is(err, marketwire.ErrSubscription), errors.Is(err, marketbook.ErrInvalidBook), errors.Is(err, marketbook.ErrIdentity):
		return ReasonPayload
	default:
		return ReasonTransport
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
