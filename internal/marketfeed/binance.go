package marketfeed

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

func (f *Feed) binance(ctx context.Context, s *socket, book *marketbook.Book, observer Observer, connection int64) error {
	first, err := s.next(ctx)
	if err != nil {
		return err
	}
	frame, err := marketwire.BinanceDepth(ctx, first.data, f.config.Instrument, first.received)
	if err != nil {
		return err
	}
	// The reader is already buffering when REST starts. A lagging REST replica
	// gets at most three fresh snapshots, without losing the first stream event.
	for attempt := range 3 {
		snapshot, err := f.snapshot(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return err
		}
		if snapshot.Sequence < frame.First-1 {
			if attempt == 2 {
				return marketbook.ErrSequenceGap
			}
			if err := wait(ctx, f.config.RetryDelay); err != nil {
				return err
			}
			continue
		}
		if err := book.Apply(snapshot); err != nil {
			return err
		}
		break
	}
	for {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		if err := f.apply(ctx, book, frame, observer, connection); err != nil {
			return err
		}
		next, err := s.next(ctx)
		if err != nil {
			return err
		}
		frame, err = marketwire.BinanceDepth(ctx, next.data, f.config.Instrument, next.received)
		if err != nil {
			return err
		}
	}
}

func (f *Feed) snapshot(ctx context.Context) (marketbook.Frame, error) {
	requestURL, err := url.Parse(f.restURL)
	if err != nil {
		return marketbook.Frame{}, ErrConfig
	}
	query := requestURL.Query()
	query.Set("symbol", f.config.Instrument.Symbol)
	query.Set("limit", strconv.Itoa(f.config.SnapshotDepth))
	requestURL.RawQuery = query.Encode()
	call, cancel := context.WithTimeout(ctx, f.config.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return marketbook.Frame{}, ErrConfig
	}
	response, err := f.http.Do(request)
	if err != nil {
		return marketbook.Frame{}, &safeError{
			class: ErrSession,
			cause: err,
		}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return marketbook.Frame{}, ErrSession
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaximumBytes+1))
	if err != nil {
		return marketbook.Frame{}, ErrSession
	}
	return marketwire.BinanceSnapshot(call, data, f.config.Instrument, time.Now().UTC())
}
