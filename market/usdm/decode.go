// Package usdm consumes complete Binance USDm top-twenty observations.
package usdm

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type depthFrame struct {
	Event    string     `json:"e"`
	Time     int64      `json:"E"`
	Symbol   string     `json:"s"`
	First    int64      `json:"U"`
	Sequence int64      `json:"u"`
	Bids     [][]string `json:"b"`
	Asks     [][]string `json:"a"`
}

// Decode validates a whole partial-depth snapshot; it never merges a missing
// side from a previous frame or treats malformed data as a heartbeat.
func Decode(data []byte, symbol string, received time.Time) (*commonv1.VenueBook, error) {
	var frame depthFrame
	if len(data) > 64*1024 || json.Unmarshal(data, &frame) != nil || frame.Event != "depthUpdate" ||
		frame.Symbol != symbol || !native.Symbol(symbol) || frame.First <= 0 || frame.Sequence < frame.First ||
		frame.Time <= 0 || frame.Time > received.Add(500*time.Millisecond).UnixMilli() || received.UnixMilli()-frame.Time >= native.BookAge.Milliseconds() {
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
	return &commonv1.VenueBook{
		Symbol:       symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		Sequence:     frame.Sequence,
		EventAtMs:    frame.Time,
		ReceivedAtMs: received.UnixMilli(),
		ValidUntilMs: min(frame.Time+native.BookAge.Milliseconds(), received.Add(native.BookAge).UnixMilli()),
		Bids:         bids,
		Asks:         asks,
	}, nil
}

func levels(rows [][]string, bids bool) ([]*commonv1.VenueLevel, error) {
	if len(rows) == 0 || len(rows) > 20 {
		return nil, native.ErrInvalid
	}
	result := make([]*commonv1.VenueLevel, 0, len(rows))
	previous := decimal.Zero
	for index, row := range rows {
		if len(row) != 2 {
			return nil, native.ErrInvalid
		}
		price, e1 := native.Value(row[0], false, false)
		_, e2 := native.Value(row[1], false, false)
		if e1 != nil || e2 != nil || index > 0 && (bids && !price.LessThan(previous) || !bids && !price.GreaterThan(previous)) {
			return nil, native.ErrInvalid
		}
		result = append(result, &commonv1.VenueLevel{
			Price:    row[0],
			Quantity: row[1],
		})
		previous = price
	}
	return result, nil
}
