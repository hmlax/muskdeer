package native

import (
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
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
func ValidateSyntheticConfig(h *commonv1.VenuePolicy) error {
	if h == nil || ValidateSyntheticSettings(&configv1.VenueRuntime{
		QuantityFraction: h.QuantityFraction,
		SyntheticTape:    h.SyntheticTape,
	}) != nil {
		return ErrInvalid
	}
	s := h.GetSyntheticTape()
	if s == nil {
		return nil
	}
	if len(s.Profiles) > len(h.Profiles) {
		return ErrInvalid
	}
	for _, p := range s.Profiles {
		market := SyntheticMarket(h, p.GetSymbol())
		quantity, e1 := Value(p.GetQuantity(), false, false)
		step, e2 := Value(market.GetQuantityStep(), false, false)
		if market == nil || e1 != nil || e2 != nil {
			return ErrInvalid
		}
		if _, remainder := quantity.QuoRem(step, 0); !remainder.IsZero() {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateSyntheticSettings validates Nacos controls before database access.
// Source bindings and lot alignment are validated against the database later.
func ValidateSyntheticSettings(h *configv1.VenueRuntime) error {
	s := h.GetSyntheticTape()
	if s == nil {
		return nil
	}
	if !validSyntheticMode(h) {
		return ErrInvalid
	}
	if !s.DevelopmentOnly || len(s.RunId) == 0 || len(s.RunId) > 20 || s.MaximumEvents == 0 || s.MaximumEvents > 1000 ||
		s.Interval.CheckValid() != nil || s.Interval.AsDuration() < 100*time.Millisecond || s.Interval.AsDuration() > time.Minute || len(s.Profiles) == 0 || len(s.Profiles) > 8 {
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
		quantity, e1 := Value(p.GetQuantity(), false, false)
		maximum, e2 := Value(p.GetMaximumNotional(), false, false)
		if !Symbol(p.GetSymbol()) || seen[p.Symbol] || e1 != nil || e2 != nil || maximum.IsZero() || quantity.IsZero() {
			return ErrInvalid
		}
		seen[p.Symbol] = true
	}
	return nil
}

func validSyntheticMode(h *configv1.VenueRuntime) bool {
	switch h.GetSyntheticTape().GetMode() {
	case configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK:
		return true
	case configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE:
		fraction, err := Value(h.QuantityFraction, false, false)
		return err == nil && !fraction.GreaterThan(decimal.NewFromInt(1))
	default:
		return false
	}
}

// SyntheticMarket returns the immutable market grid selected for the symbol.
func SyntheticMarket(h *commonv1.VenuePolicy, symbol string) *commonv1.VenueProfilePolicy {
	for _, profile := range h.GetProfiles() {
		if profile.Symbol == symbol {
			return profile
		}
	}
	return nil
}

// SyntheticProfile selects explicit per-symbol market-data limits.
func SyntheticProfile(h *commonv1.VenuePolicy, symbol string) *configv1.SyntheticTapeProfile {
	for _, profile := range h.GetSyntheticTape().GetProfiles() {
		if profile.Symbol == symbol {
			return profile
		}
	}
	return nil
}

// SyntheticPlan validates and prices a development slot from a fresh source.
func SyntheticPlan(h *commonv1.VenuePolicy, symbol string, sequence uint32, book *commonv1.VenueBook, strategy *commonv1.VenueAuthority) (*commonv1.SyntheticTapePlan, error) {
	if h.GetSyntheticTape().GetMode() != configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK || ValidateSyntheticConfig(h) != nil || !h.GetSyntheticTape().GetEnabled() || sequence >= h.SyntheticTape.MaximumEvents ||
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
