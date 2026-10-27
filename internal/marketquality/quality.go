// Package marketquality evaluates one source/product without pooling another
// source's prices. It owns no goroutine, network resource or publication lease.
package marketquality

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

// Policy makes units and recovery bounds explicit. BPS values use decimal.
type Policy = domain.MarketPolicy

// Tracker is a single actor. Only accepted prices update an established
// baseline; outliers and reconnections cannot replace it implicitly.
type Tracker struct {
	source     domain.MarketSource
	instrument marketwire.Instrument
	mode       domain.MarketMode
	policy     Policy
	current    domain.MarketReading
	last       marketbook.View
	connection int64
	baseline   decimal.Decimal
	candidate  decimal.Decimal
	samples    int
	started    time.Time
	lastNow    time.Time
}

// DefaultPolicy is conservative simulation configuration, not a production SLA.
func DefaultPolicy() Policy {
	return Policy{
		MaximumAge:      2 * time.Second,
		MaximumSkew:     500 * time.Millisecond,
		MaximumSpreadBP: decimal.NewFromInt(100),
		MaximumJumpBP:   decimal.NewFromInt(200),
		RecoverySamples: 3,
		RecoveryWindow:  200 * time.Millisecond,
	}
}

// New binds an eligible exact source/product/mode. Saving another instance's
// source does not alter this tracker, its recovery samples or its baseline.
func New(source domain.MarketSource, instrument marketwire.Instrument, mode domain.MarketMode, policy Policy) (*Tracker, error) {
	if _, err := marketbook.New(source, instrument.ID, instrument.Symbol, marketbook.MaximumLevels); err != nil {
		return nil, err
	}
	if (mode != domain.MarketModeLive && mode != domain.MarketModeReplay) || policy.Validate() != nil {
		return nil, domain.ErrUnreliableMarket
	}
	return &Tracker{
		source:     source,
		instrument: instrument,
		mode:       mode,
		policy:     policy,
	}, nil
}

// Observe checks provenance, queue age, clocks, spread and a source-local jump
// baseline. Repeated observations cannot accumulate recovery samples.
func (t *Tracker) Observe(view marketbook.View, connection int64, now time.Time) (domain.MarketReading, error) {
	if view.Source != t.source || view.InstrumentID != t.instrument.ID || view.Symbol != t.instrument.Symbol {
		return domain.MarketReading{}, domain.ErrMarketIdentity
	}
	if connection <= 0 || view.Generation <= 0 || view.Sequence < 0 {
		return t.Invalidate(domain.MarketReasonPayload, now), domain.ErrUnreliableMarket
	}
	if t.older(view, connection) {
		return domain.MarketReading{}, domain.ErrOldMarketReading
	}
	if connection == t.connection && view.Generation == t.last.Generation && view.Sequence == t.last.Sequence {
		if !samePrices(view, t.last) {
			return t.Invalidate(domain.MarketReasonPayload, now), domain.ErrUnreliableMarket
		}
		if view.ReceivedAt.Equal(t.last.ReceivedAt) {
			return t.Current(now), nil
		}
	}
	newSample := connection != t.connection || view.Generation != t.last.Generation || view.Sequence != t.last.Sequence
	if connection != t.connection || view.Generation != t.last.Generation {
		t.samples, t.started = 0, time.Time{}
	}
	t.connection, t.last = connection, view
	reading := t.reading(view, connection, now)
	if quality, reason := t.validity(reading, now); quality != domain.MarketQualityHealthy {
		return t.reject(reading, quality, reason), nil
	}
	if !t.baseline.IsZero() && excessiveJump(view.Mid, t.baseline, t.policy.MaximumJumpBP) {
		return t.reject(reading, domain.MarketQualityOutlier, domain.MarketReasonJump), nil
	}
	if t.baseline.IsZero() && !t.candidate.IsZero() && excessiveJump(view.Mid, t.candidate, t.policy.MaximumJumpBP) {
		t.samples, t.started = 0, time.Time{}
	}
	if t.samples == 0 {
		t.started, t.candidate = now, view.Mid
	}
	if newSample && t.samples < t.policy.RecoverySamples {
		t.samples++
	}
	t.lastNow = now
	if t.samples < t.policy.RecoverySamples || now.Sub(t.started) < t.policy.RecoveryWindow {
		reading.Quality, reading.Reason = domain.MarketQualityWarming, domain.MarketReasonRecovery
		t.current = withoutPrices(reading)
		return t.current, nil
	}
	t.baseline = view.Mid
	t.current = reading
	return reading, nil
}

func (t *Tracker) older(view marketbook.View, connection int64) bool {
	return connection < t.connection || connection == t.connection &&
		(view.Generation < t.last.Generation || view.Generation == t.last.Generation && view.Sequence < t.last.Sequence)
}

// Invalidate removes all executable prices but preserves the accepted baseline.
func (t *Tracker) Invalidate(reason domain.MarketReason, now time.Time) domain.MarketReading {
	reading := t.current
	if reading.Source == domain.MarketSourceUnspecified {
		reading = t.reading(marketbook.View{}, t.connection, now)
	}
	reading.EvaluatedAt = now.UTC()
	quality := domain.MarketQualityInvalid
	if reason == domain.MarketReasonSynchronizing || reason == domain.MarketReasonDisconnected {
		quality = domain.MarketQualityUnavailable
	}
	return t.reject(reading, quality, reason)
}

// Current rechecks age even when the network has sent no new book message.
func (t *Tracker) Current(now time.Time) domain.MarketReading {
	reading := t.current
	if reading.Source == domain.MarketSourceUnspecified {
		return t.Invalidate(domain.MarketReasonSynchronizing, now)
	}
	reading.EvaluatedAt = now.UTC()
	if quality, reason := t.timeQuality(reading, now); quality != domain.MarketQualityHealthy {
		return t.reject(reading, quality, reason)
	}
	t.lastNow, t.current = now, reading
	return reading
}

func (t *Tracker) reading(view marketbook.View, connection int64, now time.Time) domain.MarketReading {
	expires := view.EventTime.Add(t.policy.MaximumAge)
	if receiveExpiry := view.ReceivedAt.Add(t.policy.MaximumAge); receiveExpiry.Before(expires) {
		expires = receiveExpiry
	}
	return domain.MarketReading{
		Source:        t.source,
		InstrumentID:  t.instrument.ID,
		Symbol:        t.instrument.Symbol,
		Mode:          t.mode,
		SourceVersion: domain.MarketSourceVersion,
		Connection:    connection,
		Generation:    view.Generation,
		Sequence:      view.Sequence,
		Quality:       domain.MarketQualityHealthy,
		Reason:        domain.MarketReasonNone,
		Bid:           view.Bid.Price,
		BidQuantity:   view.Bid.Quantity,
		Ask:           view.Ask.Price,
		AskQuantity:   view.Ask.Quantity,
		Mid:           view.Mid,
		EventTime:     view.EventTime.UTC(),
		ReceivedAt:    view.ReceivedAt.UTC(),
		EvaluatedAt:   now.UTC(),
		ValidUntil:    expires.UTC(),
	}
}

func (t *Tracker) validity(reading domain.MarketReading, now time.Time) (domain.MarketQuality, domain.MarketReason) {
	if quality, reason := t.timeQuality(reading, now); quality != domain.MarketQualityHealthy {
		return quality, reason
	}
	if reading.Selected(t.source, t.instrument.ID, t.instrument.Symbol, t.mode, now, t.policy.MaximumAge) != nil {
		return domain.MarketQualityInvalid, domain.MarketReasonPayload
	}
	if reading.Ask.Sub(reading.Bid).Mul(decimal.NewFromInt(10000)).GreaterThan(reading.Mid.Mul(t.policy.MaximumSpreadBP)) {
		return domain.MarketQualitySpread, domain.MarketReasonSpread
	}
	return domain.MarketQualityHealthy, domain.MarketReasonNone
}

func (t *Tracker) timeQuality(reading domain.MarketReading, now time.Time) (domain.MarketQuality, domain.MarketReason) {
	if now.IsZero() || !t.lastNow.IsZero() && now.Before(t.lastNow) {
		return domain.MarketQualityClock, domain.MarketReasonClockReversed
	}
	if reading.EventTime.After(now.Add(t.policy.MaximumSkew)) || reading.ReceivedAt.After(now.Add(t.policy.MaximumSkew)) {
		return domain.MarketQualityClock, domain.MarketReasonFuture
	}
	if reading.EventTime.IsZero() || reading.ReceivedAt.IsZero() || !now.Before(reading.ValidUntil) ||
		now.Sub(reading.EventTime) >= t.policy.MaximumAge || now.Sub(reading.ReceivedAt) >= t.policy.MaximumAge {
		return domain.MarketQualityStale, domain.MarketReasonAge
	}
	return domain.MarketQualityHealthy, domain.MarketReasonNone
}

func (t *Tracker) reject(reading domain.MarketReading, quality domain.MarketQuality, reason domain.MarketReason) domain.MarketReading {
	t.samples, t.started = 0, time.Time{}
	if reading.EvaluatedAt.After(t.lastNow) {
		t.lastNow = reading.EvaluatedAt
	}
	reading.Quality, reading.Reason = quality, reason
	t.current = withoutPrices(reading)
	return t.current
}

func excessiveJump(value, baseline, maximum decimal.Decimal) bool {
	return value.Sub(baseline).Abs().Mul(decimal.NewFromInt(10000)).GreaterThan(baseline.Mul(maximum))
}

func samePrices(a, b marketbook.View) bool {
	return a.Bid.Price.Equal(b.Bid.Price) && a.Bid.Quantity.Equal(b.Bid.Quantity) &&
		a.Ask.Price.Equal(b.Ask.Price) && a.Ask.Quantity.Equal(b.Ask.Quantity) && a.Mid.Equal(b.Mid)
}

func withoutPrices(reading domain.MarketReading) domain.MarketReading {
	reading.Bid, reading.Ask, reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	reading.BidQuantity, reading.AskQuantity = decimal.Zero, decimal.Zero
	return reading
}
