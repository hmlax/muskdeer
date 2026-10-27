package marketworker

import (
	"context"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketfeed"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

// ReplayConfig contains explicit simulator inputs, never a live-source fallback.
type ReplayConfig struct {
	Bid             decimal.Decimal
	Ask             decimal.Decimal
	Quantity        decimal.Decimal
	Interval        time.Duration
	CallbackTimeout time.Duration
}

type replay struct {
	feed   domain.MarketFeedDefinition
	config ReplayConfig
}

// NewLive constructs the fixed public TLS adapter only for a LIVE binding.
func NewLive(feed domain.MarketFeedDefinition, timeout time.Duration) (Stream, error) {
	if feed.Validate() != nil || feed.Mode != domain.MarketModeLive {
		return nil, ErrConfig
	}
	config := marketfeed.DefaultConfig(feed.Source, marketwire.Instrument{
		ID:     feed.InstrumentID,
		Symbol: feed.Symbol,
	})
	config.HandshakeTimeout, config.RequestTimeout = timeout, timeout
	return marketfeed.New(config)
}

// NewReplay explicitly builds constant-price, sequenced spot books in REPLAY.
// It starts no task, creates no network connection and authorizes no funds.
func NewReplay(feed domain.MarketFeedDefinition, config ReplayConfig) (Stream, error) {
	if feed.Validate() != nil || feed.Mode != domain.MarketModeReplay ||
		!domain.FixedDecimal(config.Bid, 20, 18, false) || !domain.FixedDecimal(config.Ask, 20, 18, false) ||
		!domain.FixedDecimal(config.Quantity, 20, 18, false) || !config.Bid.LessThan(config.Ask) ||
		config.Interval < 10*time.Millisecond || config.Interval > time.Second || config.Interval >= feed.Policy.MaximumAge ||
		config.CallbackTimeout < 100*time.Millisecond || config.CallbackTimeout > 5*time.Second {
		return nil, ErrConfig
	}
	return &replay{
		feed:   feed,
		config: config,
	}, nil
}

func (r *replay) Run(ctx context.Context, observe marketfeed.Observer) error {
	if ctx == nil || observe == nil {
		return ErrConfig
	}
	if err := r.emit(ctx, observe, marketfeed.Observation{
		Source:       r.feed.Source,
		InstrumentID: r.feed.InstrumentID,
		Symbol:       r.feed.Symbol,
		Connection:   1,
		State:        marketbook.StateSynchronizing,
		Reason:       marketfeed.ReasonNone,
	}); err != nil {
		return err
	}
	book, err := marketbook.New(r.feed.Source, r.feed.InstrumentID, r.feed.Symbol, marketbook.MaximumLevels)
	if err != nil {
		return err
	}
	frame := r.frame(0, true)
	if err := book.Apply(frame); err != nil {
		return err
	}
	if r.feed.Source == domain.MarketSourceOKX {
		if err := r.ready(ctx, observe, book); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()
	for sequence := int64(1); sequence < math.MaxInt64; sequence++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := book.Apply(r.frame(sequence, false)); err != nil {
				return err
			}
			if err := r.ready(ctx, observe, book); err != nil {
				return err
			}
		}
	}
	return ErrEnded
}

func (r *replay) ready(ctx context.Context, observe marketfeed.Observer, book *marketbook.Book) error {
	view, err := book.Top()
	if err != nil {
		return err
	}
	return r.emit(ctx, observe, marketfeed.Observation{
		Source:       r.feed.Source,
		InstrumentID: r.feed.InstrumentID,
		Symbol:       r.feed.Symbol,
		Connection:   1,
		State:        marketbook.StateReady,
		Reason:       marketfeed.ReasonNone,
		View:         view,
	})
}

func (r *replay) emit(ctx context.Context, observe marketfeed.Observer, observation marketfeed.Observation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, r.config.CallbackTimeout)
	defer cancel()
	if err := observe(call, observation); err != nil {
		return err
	}
	return call.Err()
}

func (r *replay) frame(sequence int64, snapshot bool) marketbook.Frame {
	now := time.Now().UTC()
	return marketbook.Frame{
		Source:       r.feed.Source,
		InstrumentID: r.feed.InstrumentID,
		Symbol:       r.feed.Symbol,
		Snapshot:     snapshot,
		First:        sequence,
		Previous:     sequence - 1,
		Sequence:     sequence,
		Bids: []marketbook.Level{{
			Price:    r.config.Bid,
			Quantity: r.config.Quantity,
		}},
		Asks: []marketbook.Level{{
			Price:    r.config.Ask,
			Quantity: r.config.Quantity,
		}},
		EventTime:  now,
		ReceivedAt: now,
	}
}
