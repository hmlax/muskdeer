package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// MarketMode keeps deterministic replay distinguishable from live public data.
type MarketMode string

// MarketQuality is authorization-neutral: even HEALTHY needs a valid publisher
// lease, matching instance configuration and fresh account risk before orders.
type MarketQuality string

// MarketReason is a bounded, provider-independent diagnostic, never raw JSON.
type MarketReason string

// Source version identifies this normalized spot books contract.
const (
	MarketSourceVersion       int64         = 1
	MarketModeLive            MarketMode    = "LIVE"
	MarketModeReplay          MarketMode    = "REPLAY"
	MarketQualityHealthy      MarketQuality = "HEALTHY"
	MarketQualityUnavailable  MarketQuality = "UNAVAILABLE"
	MarketQualityWarming      MarketQuality = "WARMING"
	MarketQualityInvalid      MarketQuality = "INVALID"
	MarketQualityStale        MarketQuality = "STALE"
	MarketQualityOutlier      MarketQuality = "OUTLIER"
	MarketQualitySpread       MarketQuality = "WIDE_SPREAD"
	MarketQualityClock        MarketQuality = "TIME_SKEW"
	MarketReasonNone          MarketReason  = ""
	MarketReasonDisconnected  MarketReason  = "DISCONNECTED"
	MarketReasonSynchronizing MarketReason  = "SYNCHRONIZING"
	MarketReasonSequence      MarketReason  = "SEQUENCE"
	MarketReasonPayload       MarketReason  = "PAYLOAD"
	MarketReasonOverflow      MarketReason  = "OVERFLOW"
	MarketReasonAge           MarketReason  = "AGE"
	MarketReasonFuture        MarketReason  = "FUTURE_TIME"
	MarketReasonClockReversed MarketReason  = "CLOCK_REVERSED"
	MarketReasonJump          MarketReason  = "PRICE_JUMP"
	MarketReasonSpread        MarketReason  = "SPREAD"
	MarketReasonRecovery      MarketReason  = "RECOVERY_SAMPLES"
	MessageUnreliableMarket                 = "selected market data is unavailable or unreliable"
	MessageMarketIdentity                   = "market reading does not match the selected source or product"
	MessageOldMarketReading                 = "market reading belongs to an older connection or book"
)

// Market errors never imply a substitute source.
var (
	ErrUnreliableMarket = errors.New(MessageUnreliableMarket)
	ErrMarketIdentity   = errors.New(MessageMarketIdentity)
	ErrOldMarketReading = errors.New(MessageOldMarketReading)
)

// MarketReading contains only normalized values. Unhealthy readings carry zero
// prices. Publisher fencing is attached separately, not invented by this value.
type MarketReading struct {
	Source        MarketSource
	InstrumentID  int64
	Symbol        string
	Mode          MarketMode
	SourceVersion int64
	Connection    int64
	Generation    int64
	Sequence      int64
	Quality       MarketQuality
	Reason        MarketReason
	Bid           decimal.Decimal
	BidQuantity   decimal.Decimal
	Ask           decimal.Decimal
	AskQuantity   decimal.Decimal
	Mid           decimal.Decimal
	EventTime     time.Time
	ReceivedAt    time.Time
	EvaluatedAt   time.Time
	ValidUntil    time.Time
}

// Selected checks exact source/product/mode and freshness at the use boundary.
// It does not trust a previously persisted HEALTHY flag after its deadline.
func (r MarketReading) Selected(source MarketSource, instrumentID int64, symbol string, mode MarketMode, now time.Time, maximumAge time.Duration) error {
	if r.Source != source || r.InstrumentID != instrumentID || r.Symbol != symbol || r.Mode != mode {
		return ErrMarketIdentity
	}
	if !r.validIdentity() || r.Quality != MarketQualityHealthy || r.Reason != MarketReasonNone || !r.freshAt(now, maximumAge) || !r.validNumbers() {
		return ErrUnreliableMarket
	}
	return nil
}

func (r MarketReading) validIdentity() bool {
	return r.Source.ValidateSelection() == nil && r.InstrumentID > 0 && r.Symbol != "" &&
		(r.Mode == MarketModeLive || r.Mode == MarketModeReplay) && r.SourceVersion == MarketSourceVersion &&
		r.Connection > 0 && r.Generation > 0 && r.Sequence >= 0
}

func (r MarketReading) freshAt(now time.Time, maximumAge time.Duration) bool {
	return maximumAge > 0 && maximumAge <= 5*time.Second && now.UnixMilli() > 0 &&
		r.EventTime.UnixMilli() > 0 && r.ReceivedAt.UnixMilli() > 0 && r.EvaluatedAt.UnixMilli() > 0 &&
		now.Before(r.ValidUntil) && now.Sub(r.EventTime) < maximumAge && now.Sub(r.ReceivedAt) < maximumAge &&
		!r.EventTime.After(now.Add(time.Second)) && !r.ReceivedAt.After(now.Add(time.Second)) && !r.EvaluatedAt.After(now.Add(time.Second))
}

func (r MarketReading) validNumbers() bool {
	return FixedDecimal(r.Bid, 20, 18, false) && FixedDecimal(r.Ask, 20, 18, false) &&
		FixedDecimal(r.BidQuantity, 20, 18, false) && FixedDecimal(r.AskQuantity, 20, 18, false) &&
		FixedDecimal(r.Mid, 40, 36, false) && r.Bid.LessThan(r.Ask) && r.Mid.Mul(decimal.NewFromInt(2)).Equal(r.Bid.Add(r.Ask))
}
