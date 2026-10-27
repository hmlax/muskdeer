// Package marketwire decodes exact typed public spot market protocols.
package marketwire

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
)

// Wire variants are explicit, independently of execution venue or strategy.
const (
	BinanceEventDepthUpdate = "depthUpdate"
	OKXChannelBooks         = "books"
	OKXActionSnapshot       = "snapshot"
	OKXActionUpdate         = "update"
	MessageInvalidPayload   = "market payload fields or decimal strings are invalid"
	okxUnusedLevelField     = "0"
)

// ErrInvalidPayload requires resynchronization rather than partial application.
var ErrInvalidPayload = errors.New(MessageInvalidPayload)

// Instrument binds HTTP request/WS subscription identity to the local product.
type Instrument struct {
	ID     int64
	Symbol string
}

type binanceSnapshot struct {
	LastUpdateID *int64      `json:"lastUpdateId"`
	Bids         *[][]string `json:"bids"`
	Asks         *[][]string `json:"asks"`
}

type binanceDepth struct {
	EventType string      `json:"e"`
	EventMS   *int64      `json:"E"`
	Symbol    string      `json:"s"`
	First     *int64      `json:"U"`
	Last      *int64      `json:"u"`
	Bids      *[][]string `json:"b"`
	Asks      *[][]string `json:"a"`
}

type okxArgument struct {
	Channel    string `json:"channel"`
	Instrument string `json:"instId"`
}

type okxDepth struct {
	Argument *okxArgument `json:"arg"`
	Action   string       `json:"action"`
	Data     []okxBook    `json:"data"`
}

type okxBook struct {
	Bids     *[][]string `json:"bids"`
	Asks     *[][]string `json:"asks"`
	TimeMS   string      `json:"ts"`
	Checksum *int64      `json:"checksum"`
	Previous *int64      `json:"prevSeqId"`
	Sequence *int64      `json:"seqId"`
}

// BinanceSnapshot binds a REST response to its original requested instrument;
// Binance supplies no event timestamp or symbol in this response.
func BinanceSnapshot(ctx context.Context, data []byte, instrument Instrument, received time.Time) (marketbook.Frame, error) {
	raw, err := decode[binanceSnapshot](ctx, data)
	if err != nil {
		return marketbook.Frame{}, err
	}
	if !validScope(instrument, received) || raw.LastUpdateID == nil || *raw.LastUpdateID < 0 || raw.Bids == nil || raw.Asks == nil {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	frame := marketbook.Frame{
		Source:       domain.MarketSourceBinance,
		InstrumentID: instrument.ID,
		Symbol:       instrument.Symbol,
		Snapshot:     true,
		Previous:     -1,
		Sequence:     *raw.LastUpdateID,
		ReceivedAt:   received.UTC(),
	}
	return withLevels(ctx, frame, *raw.Bids, *raw.Asks, 2)
}

// BinanceDepth decodes the raw spot depthUpdate stream, not futures or combined
// stream envelopes. Decimal JSON numbers and missing arrays are rejected.
func BinanceDepth(ctx context.Context, data []byte, instrument Instrument, received time.Time) (marketbook.Frame, error) {
	raw, err := decode[binanceDepth](ctx, data)
	if err != nil {
		return marketbook.Frame{}, err
	}
	if !validScope(instrument, received) || raw.EventType != BinanceEventDepthUpdate || raw.Symbol != instrument.Symbol ||
		raw.EventMS == nil || *raw.EventMS <= 0 || raw.First == nil || *raw.First <= 0 || raw.Last == nil || *raw.Last < *raw.First || raw.Bids == nil || raw.Asks == nil {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	frame := marketbook.Frame{
		Source:       domain.MarketSourceBinance,
		InstrumentID: instrument.ID,
		Symbol:       instrument.Symbol,
		First:        *raw.First,
		Sequence:     *raw.Last,
		EventTime:    time.UnixMilli(*raw.EventMS).UTC(),
		ReceivedAt:   received.UTC(),
	}
	return withLevels(ctx, frame, *raw.Bids, *raw.Asks, 2)
}

// OKXDepth decodes public books snapshots/updates. The deprecated checksum is
// typed metadata only and never determines integrity; the book checks sequences.
func OKXDepth(ctx context.Context, data []byte, instrument Instrument, received time.Time) (marketbook.Frame, error) {
	raw, err := decode[okxDepth](ctx, data)
	if err != nil {
		return marketbook.Frame{}, err
	}
	return okxFrame(ctx, raw, instrument, received)
}

func okxFrame(ctx context.Context, raw okxDepth, instrument Instrument, received time.Time) (marketbook.Frame, error) {
	if !validScope(instrument, received) || raw.Argument == nil || raw.Argument.Channel != OKXChannelBooks || raw.Argument.Instrument != instrument.Symbol ||
		(raw.Action != OKXActionSnapshot && raw.Action != OKXActionUpdate) || len(raw.Data) != 1 {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	book := raw.Data[0]
	if book.Sequence == nil || *book.Sequence < 0 || book.Previous == nil || book.Bids == nil || book.Asks == nil {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	timestamp, err := strconv.ParseInt(book.TimeMS, 10, 64)
	if err != nil || timestamp <= 0 || strconv.FormatInt(timestamp, 10) != book.TimeMS {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	snapshot := raw.Action == OKXActionSnapshot
	if snapshot && *book.Previous != -1 || !snapshot && *book.Previous < 0 {
		return marketbook.Frame{}, ErrInvalidPayload
	}
	frame := marketbook.Frame{
		Source:       domain.MarketSourceOKX,
		InstrumentID: instrument.ID,
		Symbol:       instrument.Symbol,
		Snapshot:     snapshot,
		Previous:     *book.Previous,
		Sequence:     *book.Sequence,
		EventTime:    time.UnixMilli(timestamp).UTC(),
		ReceivedAt:   received.UTC(),
	}
	return withLevels(ctx, frame, *book.Bids, *book.Asks, 4)
}

func withLevels(ctx context.Context, frame marketbook.Frame, bids, asks [][]string, width int) (marketbook.Frame, error) {
	var err error
	frame.Bids, err = levels(ctx, bids, width, frame.Snapshot)
	if err != nil {
		return marketbook.Frame{}, err
	}
	frame.Asks, err = levels(ctx, asks, width, frame.Snapshot)
	if err != nil {
		return marketbook.Frame{}, err
	}
	return frame, nil
}

func levels(ctx context.Context, rows [][]string, width int, snapshot bool) ([]marketbook.Level, error) {
	if len(rows) > marketbook.MaximumLevels || snapshot && len(rows) == 0 {
		return nil, ErrInvalidPayload
	}
	result := make([]marketbook.Level, 0, len(rows))
	priceRule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	quantityRule := priceRule
	quantityRule.AllowZero = !snapshot
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(row) != width {
			return nil, ErrInvalidPayload
		}
		price, err := domain.ParseDecimal(row[0], priceRule)
		if err != nil {
			return nil, ErrInvalidPayload
		}
		quantity, err := domain.ParseDecimal(row[1], quantityRule)
		if err != nil {
			return nil, ErrInvalidPayload
		}
		if width == 4 {
			count, err := strconv.ParseUint(row[3], 10, 64)
			if err != nil || strconv.FormatUint(count, 10) != row[3] || row[2] != okxUnusedLevelField {
				return nil, ErrInvalidPayload
			}
		}
		result = append(result, marketbook.Level{
			Price:    price,
			Quantity: quantity,
		})
	}
	return result, nil
}

func validScope(instrument Instrument, received time.Time) bool {
	return instrument.ID > 0 && len(instrument.Symbol) > 0 && len(instrument.Symbol) <= 64 && !received.IsZero() && received.UnixMilli() > 0
}
