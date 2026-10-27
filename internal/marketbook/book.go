// Package marketbook maintains bounded, single-owner provider order books.
// It starts no task; readers receive value snapshots instead of mutable maps.
package marketbook

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

// State distinguishes a complete book from a REST anchor awaiting a bridge.
type State string

// Book lifecycle and fixed public failure messages.
const (
	StateWaiting       State = "WAITING"
	StateSynchronizing State = "SYNCHRONIZING"
	StateReady         State = "READY"
	StateInvalid       State = "INVALID"
	MaximumLevels            = 5000
	MessageNotReady          = "market order book is not ready"
	MessageSequenceGap       = "market order book sequence is discontinuous"
	MessageInvalidBook       = "market order book values or shape are invalid"
	MessageIdentity          = "market update belongs to another source or instrument"
)

// Errors require explicit recovery, never substitution of another provider.
var (
	ErrNotReady    = errors.New(MessageNotReady)
	ErrSequenceGap = errors.New(MessageSequenceGap)
	ErrInvalidBook = errors.New(MessageInvalidBook)
	ErrIdentity    = errors.New(MessageIdentity)
)

// Level uses base quantity and quote/base price, already parsed without floats.
type Level struct {
	Price    decimal.Decimal
	Quantity decimal.Decimal
}

// Frame is typed provider data. First is Binance's U, Previous is OKX's
// prevSeqId. A full snapshot has Previous=-1. Times are explicit UTC instants.
type Frame struct {
	Source       domain.MarketSource
	InstrumentID int64
	Symbol       string
	Snapshot     bool
	First        int64
	Previous     int64
	Sequence     int64
	Bids         []Level
	Asks         []Level
	EventTime    time.Time
	ReceivedAt   time.Time
}

// View is a complete BBO and its provenance. Generation is local book recovery
// identity, not cross-process ownership or an execution fencing token.
type View struct {
	Source       domain.MarketSource
	InstrumentID int64
	Symbol       string
	Generation   int64
	Sequence     int64
	Bid          Level
	Ask          Level
	Mid          decimal.Decimal
	EventTime    time.Time
	ReceivedAt   time.Time
}

// Book belongs to one actor; callers must serialize Apply/Top. No map or slice
// supplied by a caller is retained, and no mutable depth map is exposed.
type Book struct {
	source       domain.MarketSource
	instrumentID int64
	symbol       string
	capacity     int
	state        State
	generation   int64
	sequence     int64
	bids         map[string]Level
	asks         map[string]Level
	lastHash     [32]byte
	top          View
}

// New binds exactly one eligible source and configured external symbol.
func New(source domain.MarketSource, instrumentID int64, symbol string, capacity int) (*Book, error) {
	if err := source.ValidateSelection(); err != nil {
		return nil, err
	}
	if instrumentID <= 0 || !validSymbol(symbol) || capacity < 1 || capacity > MaximumLevels {
		return nil, ErrIdentity
	}
	return &Book{
		source:       source,
		instrumentID: instrumentID,
		symbol:       symbol,
		capacity:     capacity,
		state:        StateWaiting,
	}, nil
}

// Apply validates a whole event before replacing depth. A gap or bad selected
// book invalidates its prices; unrelated source/instrument input is rejected.
func (b *Book) Apply(frame Frame) error {
	if frame.Source != b.source || frame.InstrumentID != b.instrumentID || frame.Symbol != b.symbol {
		return ErrIdentity
	}
	if err := frameBounds(frame); err != nil {
		return b.invalidate(err)
	}
	if frame.Snapshot {
		return b.snapshot(frame)
	}
	if b.state != StateReady && b.state != StateSynchronizing {
		return ErrNotReady
	}
	digest := frameHash(frame)
	if b.source == domain.MarketSourceBinance {
		if frame.Sequence <= b.sequence {
			return nil
		}
		if frame.First <= 0 || frame.First > frame.Sequence || frame.First > b.sequence+1 {
			return b.invalidate(ErrSequenceGap)
		}
	} else {
		if digest == b.lastHash && frame.Sequence == b.sequence {
			return nil
		}
		if frame.Previous != b.sequence || frame.Previous < 0 || (frame.Sequence == frame.Previous && len(frame.Bids)+len(frame.Asks) != 0) {
			return b.invalidate(ErrSequenceGap)
		}
	}
	bids, asks := maps.Clone(b.bids), maps.Clone(b.asks)
	applyLevels(bids, frame.Bids)
	applyLevels(asks, frame.Asks)
	if err := b.setDepth(frame, bids, asks); err != nil {
		return b.invalidate(err)
	}
	if b.source == domain.MarketSourceOKX && frame.Sequence < frame.Previous {
		if b.generation == math.MaxInt64 {
			return b.invalidate(ErrInvalidBook)
		}
		b.generation++
		b.top.Generation = b.generation
	}
	b.state = StateReady
	b.lastHash = digest
	return nil
}

// Top returns prices only after protocol synchronization and shape validation.
// Freshness and outlier policy must still be checked before producing risk.
func (b *Book) Top() (View, error) {
	if b.state != StateReady {
		return View{}, ErrNotReady
	}
	return b.top, nil
}

// State reports synchronization state without exposing an invalid book's price.
func (b *Book) State() State { return b.state }

func (b *Book) snapshot(frame Frame) error {
	if frame.Previous != -1 || b.generation == math.MaxInt64 {
		return b.invalidate(ErrInvalidBook)
	}
	bids := make(map[string]Level, len(frame.Bids))
	asks := make(map[string]Level, len(frame.Asks))
	applyLevels(bids, frame.Bids)
	applyLevels(asks, frame.Asks)
	if err := b.setDepth(frame, bids, asks); err != nil {
		return b.invalidate(err)
	}
	b.generation++
	b.top.Generation = b.generation
	b.state = StateReady
	if b.source == domain.MarketSourceBinance {
		b.state = StateSynchronizing
	}
	b.lastHash = frameHash(frame)
	return nil
}

func (b *Book) setDepth(frame Frame, bids, asks map[string]Level) error {
	if len(bids) == 0 || len(asks) == 0 || len(bids) > b.capacity || len(asks) > b.capacity {
		return ErrInvalidBook
	}
	bid, ask := bestLevel(bids, true), bestLevel(asks, false)
	if bid.Price.GreaterThanOrEqual(ask.Price) {
		return ErrInvalidBook
	}
	mid, _ := bid.Price.Add(ask.Price).QuoRem(decimal.NewFromInt(2), 36)
	b.bids, b.asks, b.sequence = bids, asks, frame.Sequence
	b.top = View{
		Source:       b.source,
		InstrumentID: b.instrumentID,
		Symbol:       b.symbol,
		Generation:   b.generation,
		Sequence:     frame.Sequence,
		Bid:          bid,
		Ask:          ask,
		Mid:          mid,
		EventTime:    frame.EventTime.UTC(),
		ReceivedAt:   frame.ReceivedAt.UTC(),
	}
	return nil
}

func (b *Book) invalidate(err error) error {
	b.state = StateInvalid
	b.bids, b.asks = nil, nil
	b.top = View{}
	return err
}

func frameBounds(frame Frame) error {
	if frame.Sequence < 0 || frame.ReceivedAt.IsZero() || frame.ReceivedAt.UnixMilli() <= 0 ||
		((!frame.Snapshot || frame.Source == domain.MarketSourceOKX) && (frame.EventTime.IsZero() || frame.EventTime.UnixMilli() <= 0)) ||
		len(frame.Bids) > MaximumLevels || len(frame.Asks) > MaximumLevels {
		return ErrInvalidBook
	}
	if frame.Source == domain.MarketSourceBinance && !frame.Snapshot && (frame.First <= 0 || frame.First > frame.Sequence) {
		return ErrInvalidBook
	}
	for _, side := range [][]Level{frame.Bids, frame.Asks} {
		seen := make(map[string]struct{}, len(side))
		for _, level := range side {
			if !domain.FixedDecimal(level.Price, 20, 18, false) || !domain.FixedDecimal(level.Quantity, 20, 18, !frame.Snapshot) {
				return ErrInvalidBook
			}
			key := level.Price.String()
			if _, exists := seen[key]; exists {
				return ErrInvalidBook
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func validSymbol(symbol string) bool {
	if len(symbol) == 0 || len(symbol) > 64 {
		return false
	}
	for index := range len(symbol) {
		character := symbol[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func applyLevels(depth map[string]Level, levels []Level) {
	for _, level := range levels {
		key := level.Price.String()
		if level.Quantity.IsZero() {
			delete(depth, key)
			continue
		}
		depth[key] = level
	}
}

func bestLevel(depth map[string]Level, bid bool) Level {
	var best Level
	for _, level := range depth {
		if best.Price.IsZero() || bid && level.Price.GreaterThan(best.Price) || !bid && level.Price.LessThan(best.Price) {
			best = level
		}
	}
	return best
}

func frameHash(frame Frame) [32]byte {
	data := fmt.Appendf(nil, "%t:%d:%d:%d:%d|", frame.Snapshot, frame.First, frame.Previous, frame.Sequence, frame.EventTime.UnixMilli())
	for _, side := range [][]Level{frame.Bids, frame.Asks} {
		for _, level := range side {
			data = fmt.Appendf(data, "%s/%s;", level.Price.String(), level.Quantity.String())
		}
		data = append(data, '|')
	}
	return sha256.Sum256(data)
}
