package marketfeed

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

type message struct {
	data     []byte
	received time.Time
}

type socket struct {
	conn     *websocket.Conn
	messages chan message
	bytes    atomic.Int64
	config   Config
}

// session owns one reader and the cancellation callback, and joins both. The
// actor is the only application writer; control replies use concurrent-safe
// WriteControl. Read state/deadlines belong exclusively to the reader.
func (f *Feed) session(parent context.Context, observer Observer, connection int64) error {
	scope, stopScope := context.WithTimeout(parent, f.config.SessionLifetime)
	defer stopScope()
	handshake, stopHandshake := context.WithTimeout(scope, f.config.HandshakeTimeout)
	conn, response, err := f.dialer.DialContext(handshake, f.wsURL, nil)
	stopHandshake()
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return &safeError{
			class: ErrSession,
			cause: err,
		}
	}
	ctx, cancel := context.WithCancelCause(scope)
	s := &socket{
		conn:     conn,
		messages: make(chan message, f.config.BufferMessages),
		config:   f.config,
	}
	readerDone := make(chan struct{})
	closeDone := make(chan struct{})
	stopClose := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		close(closeDone)
	})
	go func() {
		defer close(readerDone)
		if err := s.read(ctx); err != nil {
			cancel(err)
		}
	}()
	defer func() {
		cancel(context.Canceled)
		_ = conn.Close()
		<-readerDone
		if !stopClose() {
			<-closeDone
		}
	}()
	book, err := marketbook.New(f.config.Source, f.config.Instrument.ID, f.config.Instrument.Symbol, marketbook.MaximumLevels)
	if err != nil {
		return err
	}
	if f.config.Source == domain.MarketSourceBinance {
		err = f.binance(ctx, s, book, observer, connection)
	} else {
		err = f.okx(ctx, s, book, observer, connection)
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return err
}

func (s *socket) read(ctx context.Context) error {
	s.conn.SetReadLimit(MaximumBytes)
	s.conn.SetPingHandler(func(payload string) error {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
			return err
		}
		return s.conn.WriteControl(websocket.PongMessage, []byte(payload), time.Now().Add(time.Second))
	})
	for {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
			return err
		}
		kind, data, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		if kind != websocket.TextMessage || len(data) == 0 || len(data) > MaximumBytes {
			return marketwire.ErrInvalidPayload
		}
		if s.bytes.Add(int64(len(data))) > s.config.BufferBytes {
			return ErrOverflow
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case s.messages <- message{
			data:     data,
			received: time.Now().UTC(),
		}:
		default:
			return ErrOverflow
		}
	}
}

func (s *socket) next(ctx context.Context) (message, error) {
	if ctx.Err() != nil {
		return message{}, context.Cause(ctx)
	}
	select {
	case <-ctx.Done():
		return message{}, context.Cause(ctx)
	case next := <-s.messages:
		s.bytes.Add(-int64(len(next.data)))
		return next, nil
	}
}

func (s *socket) write(ctx context.Context, data []byte) error {
	deadline := time.Now().Add(s.config.RequestTimeout)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, data)
}

func (f *Feed) apply(ctx context.Context, book *marketbook.Book, frame marketbook.Frame, observer Observer, connection int64) error {
	before, _ := book.Top()
	if err := book.Apply(frame); err != nil {
		return err
	}
	view, err := book.Top()
	if errors.Is(err, marketbook.ErrNotReady) {
		return nil
	}
	if err != nil {
		return err
	}
	if before.Generation == view.Generation && before.Sequence == view.Sequence && before.ReceivedAt.Equal(view.ReceivedAt) {
		return nil
	}
	return f.notify(ctx, observer, connection, marketbook.StateReady, ReasonNone, view)
}
