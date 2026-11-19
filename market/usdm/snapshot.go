package usdm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const snapshotInterval = 750 * time.Millisecond

type depthSnapshot struct {
	Sequence int64      `json:"lastUpdateId"`
	EventAt  int64      `json:"E"`
	TradeAt  int64      `json:"T"`
	Bids     [][]string `json:"bids"`
	Asks     [][]string `json:"asks"`
}

type snapshotRateLimit struct{ until time.Time }

func (e *snapshotRateLimit) Error() string { return "source snapshot rate limited" }

// A fresh HTTP observation may contain an old last-change time on a quiet
// symbol. Keep that exchange time; bound validity by the real request start.
func (f *Feed) snapshot(ctx context.Context) (*common.VenueBook, error) {
	started := time.Now()
	work, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if f.snapshotAdmission != nil {
		if err := f.snapshotAdmission.Wait(work); err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(work, http.MethodGet, f.snapshotURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	response, err := f.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusTeapot {
		delay := time.Minute
		if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 && seconds <= 86400 {
			delay = time.Duration(seconds) * time.Second
		}
		until := time.Now().Add(delay)
		if f.snapshotAdmission != nil {
			f.snapshotAdmission.backoff(until)
		}
		return nil, &snapshotRateLimit{until: until}
	}
	if response.StatusCode != http.StatusOK {
		return nil, native.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return nil, native.ErrInvalid
	}
	var frame depthSnapshot
	if json.Unmarshal(data, &frame) != nil {
		return nil, native.ErrInvalid
	}
	received := time.Now()
	if work.Err() != nil || frame.Sequence <= 0 || frame.EventAt <= 0 || frame.TradeAt <= 0 || frame.TradeAt > frame.EventAt || frame.EventAt > received.Add(500*time.Millisecond).UnixMilli() {
		return nil, native.ErrInvalid
	}
	bids, err := levels(frame.Bids, true)
	if err != nil {
		return nil, err
	}
	asks, err := levels(frame.Asks, false)
	if err != nil {
		return nil, err
	}
	bid, _ := native.Value(bids[0].Price, false, false)
	ask, _ := native.Value(asks[0].Price, false, false)
	if !bid.LessThan(ask) {
		return nil, native.ErrInvalid
	}
	return &common.VenueBook{
		Symbol:              f.symbol,
		Source:              native.Source,
		Segment:             native.Segment,
		Sequence:            frame.Sequence,
		EventAtMs:           frame.EventAt,
		ReceivedAtMs:        received.UnixMilli(),
		SnapshotStartedAtMs: started.UnixMilli(),
		ValidUntilMs:        started.Add(native.BookAge).UnixMilli(),
		Bids:                bids,
		Asks:                asks,
	}, nil
}
