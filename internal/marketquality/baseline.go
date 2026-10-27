package marketquality

import (
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

// Baseline returns a diagnostic checkpoint, never a fresh/executable quote.
func (t *Tracker) Baseline() domain.MarketBaseline {
	return domain.MarketBaseline{
		Source:        t.source,
		InstrumentID:  t.instrument.ID,
		Symbol:        t.instrument.Symbol,
		Mode:          t.mode,
		SourceVersion: domain.MarketSourceVersion,
		Mid:           t.baseline,
		AcceptedAt:    t.baselineAt,
	}
}

// Restore accepts one same-scope checkpoint before any observation/evaluation.
// It preserves no health, samples, sequence or authority from a prior process.
func (t *Tracker) Restore(checkpoint domain.MarketBaseline, now time.Time) error {
	if t.restored || t.connection != 0 || !t.lastNow.IsZero() || now.UnixMilli() <= 0 {
		return domain.ErrUnreliableMarket
	}
	if checkpoint.Source != t.source || checkpoint.InstrumentID != t.instrument.ID || checkpoint.Symbol != t.instrument.Symbol || checkpoint.Mode != t.mode || checkpoint.SourceVersion != domain.MarketSourceVersion {
		return domain.ErrMarketIdentity
	}
	if !domain.FixedDecimal(checkpoint.Mid, 40, 36, true) {
		return domain.ErrUnreliableMarket
	}
	if checkpoint.Mid.IsZero() {
		if !checkpoint.AcceptedAt.IsZero() {
			return domain.ErrUnreliableMarket
		}
	} else if !domain.FixedDecimal(checkpoint.Mid, 40, 36, false) || checkpoint.AcceptedAt.UnixMilli() <= 0 || checkpoint.AcceptedAt.After(now.Add(t.policy.MaximumSkew)) {
		return domain.ErrUnreliableMarket
	}
	t.baseline, t.baselineAt, t.restored = checkpoint.Mid, checkpoint.AcceptedAt.UTC(), true
	return nil
}
