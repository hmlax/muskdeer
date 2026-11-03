package native

import (
	"time"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// Synthetic audit states describe market-data publication, never financial fills.
const (
	SyntheticPublished = "SYNTHETIC_PUBLISHED"
	SyntheticUnknown   = "SYNTHETIC_UNKNOWN"
	SyntheticWorker    = "strategy.synthetic."
)

// SyntheticExecutionKey identifies a market-data run, never a financial fund.
func SyntheticExecutionKey(run, symbol string) string { return "ht/" + run + "/" + symbol }

// SyntheticStrategyKey separates the strategy publisher from the SDK sender.
func SyntheticStrategyKey(run, symbol string) string {
	return "hs/" + SyntheticExecutionKey(run, symbol)
}

// ValidateSyntheticConfig requires a finite, explicitly developmental run.
func ValidateSyntheticConfig(h *configv1.HimalayaRuntime) error {
	s := h.GetSyntheticTape()
	if s == nil {
		return nil
	}
	if !s.DevelopmentOnly || len(s.RunId) == 0 || len(s.RunId) > 20 || s.MaximumEvents == 0 || s.MaximumEvents > 1000 ||
		s.Interval.CheckValid() != nil || s.Interval.AsDuration() < 100*time.Millisecond || s.Interval.AsDuration() > time.Minute || len(s.Profiles) == 0 || len(s.Profiles) > len(h.Profiles) {
		return ErrInvalid
	}
	for _, c := range s.RunId {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for _, p := range s.Profiles {
		market := SyntheticMarket(h, p.GetSymbol())
		quantity, e1 := Value(p.GetQuantity(), false, false)
		maximum, e2 := Value(p.GetMaximumNotional(), false, false)
		step, e3 := Value(market.GetQuantityStep(), false, false)
		if market == nil || seen[p.Symbol] || e1 != nil || e2 != nil || e3 != nil || maximum.IsZero() {
			return ErrInvalid
		}
		if _, remainder := quantity.QuoRem(step, 0); !remainder.IsZero() {
			return ErrInvalid
		}
		seen[p.Symbol] = true
	}
	return nil
}

// SyntheticMarket returns the immutable market grid selected for the symbol.
func SyntheticMarket(h *configv1.HimalayaRuntime, symbol string) *configv1.HimalayaProfile {
	for _, profile := range h.GetProfiles() {
		if profile.Symbol == symbol {
			return profile
		}
	}
	return nil
}

// SyntheticProfile selects explicit per-symbol market-data limits.
func SyntheticProfile(h *configv1.HimalayaRuntime, symbol string) *configv1.SyntheticTapeProfile {
	for _, profile := range h.GetSyntheticTape().GetProfiles() {
		if profile.Symbol == symbol {
			return profile
		}
	}
	return nil
}

// SyntheticPlan validates and prices a development slot from a fresh source.
func SyntheticPlan(h *configv1.HimalayaRuntime, symbol string, sequence uint32, book *commonv1.HimalayaBook, strategy *commonv1.HimalayaAuthority) (*commonv1.SyntheticTapePlan, error) {
	if ValidateSyntheticConfig(h) != nil || !h.GetSyntheticTape().GetEnabled() || sequence >= h.SyntheticTape.MaximumEvents ||
		book.GetSymbol() != symbol || book.GetSource() != Source || book.GetSegment() != Segment || book.GetValidUntilMs() <= time.Now().UnixMilli() ||
		len(book.GetBids()) == 0 || len(book.GetAsks()) == 0 {
		return nil, ErrInvalid
	}
	p := SyntheticProfile(h, symbol)
	market := SyntheticMarket(h, symbol)
	if p == nil || market == nil {
		return nil, ErrInvalid
	}
	tick, e1 := Value(market.PriceTick, false, false)
	bid, e2 := Value(book.Bids[0].GetPrice(), false, false)
	ask, e3 := Value(book.Asks[0].GetPrice(), false, false)
	quantity, e4 := Value(p.Quantity, false, false)
	maximum, e5 := Value(p.MaximumNotional, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || !bid.LessThan(ask) {
		return nil, ErrInvalid
	}
	side, price, rounding := domain.SideBuy, ask, domain.SideSell
	if sequence%2 == 1 {
		side, price, rounding = domain.SideSell, bid, domain.SideBuy
	}
	price, err := domain.RoundPrice(price, tick, rounding)
	if err != nil || price.Mul(quantity).GreaterThan(maximum) {
		return nil, ErrInvalid
	}
	return &commonv1.SyntheticTapePlan{
		RunId:    h.SyntheticTape.RunId,
		Symbol:   symbol,
		Sequence: sequence,
		Side:     string(side),
		Price:    price.String(),
		Quantity: quantity.String(),
		Strategy: proto.CloneOf(strategy),
		Book:     proto.CloneOf(book),
	}, nil
}
