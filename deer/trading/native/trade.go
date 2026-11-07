package native

import (
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

const (
	// TradeAge bounds the external trade used for one synthetic publication.
	TradeAge          = 1500 * time.Millisecond
	tradeClockSkew    = 500 * time.Millisecond
	tradeMarketPrefix = "hm/trade/"
)

// TradeKey separates aggregate-trade ownership from the depth publisher.
func TradeKey(symbol string) string { return tradeMarketPrefix + symbol }

// TradeSequence requires a canonical positive exchange aggregate-trade ID.
func TradeSequence(trade *commonv1.VenueTrade) (int64, error) {
	id, err := strconv.ParseInt(trade.GetEventId(), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != trade.EventId {
		return 0, ErrInvalid
	}
	return id, nil
}

// ValidateTrade checks source identity, exact values and both exchange clocks.
// The publisher is attached and fenced separately by the market worker.
func ValidateTrade(trade *commonv1.VenueTrade, symbol string, now time.Time) error {
	if trade.GetSymbol() != symbol || !Symbol(symbol) || trade.GetSource() != Source || trade.GetSegment() != Segment ||
		trade.GetSide() != string(domain.SideBuy) && trade.GetSide() != string(domain.SideSell) {
		return ErrInvalid
	}
	if _, err := TradeSequence(trade); err != nil {
		return err
	}
	_, priceErr := Value(trade.Price, false, false)
	_, quantityErr := Value(trade.Quantity, false, false)
	upper := now.Add(tradeClockSkew).UnixMilli()
	if priceErr != nil || quantityErr != nil || trade.EventAtMs <= 0 || trade.TradeAtMs <= 0 || trade.ReceivedAtMs <= 0 ||
		trade.TradeAtMs > trade.EventAtMs || trade.EventAtMs > upper || trade.ReceivedAtMs > upper ||
		trade.ValidUntilMs <= now.UnixMilli() || trade.ValidUntilMs > min(trade.EventAtMs, trade.TradeAtMs, trade.ReceivedAtMs)+TradeAge.Milliseconds() {
		return ErrInvalid
	}
	return nil
}

// SyntheticTradeTarget scales source quantity within the existing single-event
// cap and rounds down to a lot. Too-small quantities are not manufactured.
func SyntheticTradeTarget(h *commonv1.VenuePolicy, trade *commonv1.VenueTrade) (*commonv1.VenueTarget, error) {
	profile := SyntheticProfile(h, trade.GetSymbol())
	market := SyntheticMarket(h, trade.GetSymbol())
	if profile == nil || market == nil || trade.GetSource() != Source || trade.GetSegment() != Segment {
		return nil, ErrInvalid
	}
	if _, err := TradeSequence(trade); err != nil {
		return nil, err
	}
	side := domain.Side(trade.Side)
	price, e1 := Value(trade.Price, false, false)
	quantity, e2 := Value(trade.Quantity, false, false)
	fraction, e3 := Value(h.QuantityFraction, false, false)
	tick, e4 := Value(market.PriceTick, false, false)
	step, e5 := Value(market.QuantityStep, false, false)
	capValue, e6 := Value(profile.Quantity, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil ||
		side != domain.SideBuy && side != domain.SideSell || fraction.GreaterThan(decimal.NewFromInt(1)) {
		return nil, ErrInvalid
	}
	price, err := domain.RoundPrice(price, tick, side)
	if err != nil || !price.IsPositive() {
		return nil, ErrInvalid
	}
	quantity, err = domain.RoundQuantity(decimal.Min(quantity.Mul(fraction), capValue), step)
	if err != nil || !quantity.IsPositive() {
		return nil, ErrInvalid
	}
	return &commonv1.VenueTarget{
		Side:     string(side),
		Price:    price.String(),
		Quantity: quantity.String(),
	}, nil
}

// SyntheticSourcePlan preserves book mode and requires an owned, fresh trade in
// aggregate mode. Callers cannot silently downgrade the configured source.
func SyntheticSourcePlan(h *commonv1.VenuePolicy, input *commonv1.SyntheticTapePlan) (*commonv1.SyntheticTapePlan, error) {
	if h == nil || input == nil || input.GetRunId() != h.GetSyntheticTape().GetRunId() {
		return nil, ErrInvalid
	}
	if h.GetSyntheticTape().GetMode() == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK {
		if input.SourceTrade != nil {
			return nil, ErrInvalid
		}
		plan, err := SyntheticPlan(h, input.Symbol, input.Sequence, input.Book, input.Strategy)
		if err == nil {
			plan.StrategyInstanceId, plan.StrategyRevision = input.StrategyInstanceId, input.StrategyRevision
		}
		return plan, err
	}
	book, trade := input.GetBook(), input.GetSourceTrade()
	if ValidateSyntheticConfig(h) != nil || !h.GetSyntheticTape().GetEnabled() || input.Sequence >= h.SyntheticTape.MaximumEvents ||
		book.GetSource() != Source || book.GetSegment() != Segment || book.GetSymbol() != input.Symbol || book.GetValidUntilMs() <= time.Now().UnixMilli() ||
		ValidateTrade(trade, input.Symbol, time.Now()) != nil || trade.GetPublisher().GetAccountId() != TradeKey(input.Symbol) ||
		trade.Publisher.GetOwnerId() == "" || trade.Publisher.GetEpoch() <= 0 || trade.Publisher.ExpiresAtMs < trade.ValidUntilMs {
		return nil, ErrInvalid
	}
	target, err := SyntheticTradeTarget(h, trade)
	if err != nil {
		return nil, err
	}
	maximum, _ := Value(SyntheticProfile(h, input.Symbol).MaximumNotional, false, false)
	price, _ := Value(target.Price, false, false)
	quantity, _ := Value(target.Quantity, false, false)
	if price.Mul(quantity).GreaterThan(maximum) {
		return nil, ErrInvalid
	}
	return &commonv1.SyntheticTapePlan{
		RunId:              input.RunId,
		Symbol:             input.Symbol,
		Sequence:           input.Sequence,
		Side:               target.Side,
		Price:              target.Price,
		Quantity:           target.Quantity,
		Strategy:           proto.CloneOf(input.Strategy),
		Book:               proto.CloneOf(book),
		SourceTrade:        proto.CloneOf(trade),
		StrategyInstanceId: input.StrategyInstanceId,
		StrategyRevision:   input.StrategyRevision,
	}, nil
}
