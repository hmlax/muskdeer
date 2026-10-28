// Package quoting computes funded passive targets without submitting an order
// or owning time, goroutines, database state or execution authority.
package quoting

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

// MessageInvalidInput is safe for an operation result; no input value is echoed.
const MessageInvalidInput = "maker input, selected reference or trading metadata is invalid"

// ErrInvalidInput returns no targets, leaving protection to the controller.
var ErrInvalidInput = errors.New(MessageInvalidInput)

// Instrument is current execution metadata, independent of external source.
type Instrument struct {
	ID              int64
	MetadataVersion int64
	Status          string
	PriceTick       decimal.Decimal
	QuantityStep    decimal.Decimal
	MinNotional     decimal.Decimal
}

// Input contains explicit allocation available to this plan; it is not a
// reservation. Execution must still atomically reserve funds and all limits.
type Input struct {
	Instrument     Instrument
	Source         domain.MarketSource
	Symbol         string
	Mode           domain.MarketMode
	Market         domain.MarketReference
	Parameters     domain.MakerParameters
	Inventory      decimal.Decimal
	AvailableBase  decimal.Decimal
	AvailableQuote decimal.Decimal
	QuoteFeeRate   decimal.Decimal
	LocalBid       decimal.Decimal
	LocalAsk       decimal.Decimal
	Now            time.Time
}

// Target is a desired passive base quantity, not an accepted/live core order.
type Target struct {
	Side        domain.Side
	Level       int
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	TimeInForce domain.TimeInForce
}

type allocation struct {
	quote     decimal.Decimal
	base      decimal.Decimal
	buyRoom   decimal.Decimal
	buyLimit  decimal.Decimal
	sellLimit decimal.Decimal
}

// Maker produces near-to-far interleaved buy/sell targets. Inventory tilts the
// center by up to half the base spread; each tick is rounded from an exact
// rational expression, without rounding a fractional inventory ratio first.
func Maker(input Input) ([]Target, error) {
	if err := validateInput(input); err != nil {
		return nil, errors.Join(ErrInvalidInput, err)
	}
	p := input.Parameters
	funds := allocation{
		quote:     input.AvailableQuote,
		base:      minimum(input.AvailableBase, input.Inventory),
		buyRoom:   maximum(decimal.Zero, p.InventoryHardLimit.Sub(input.Inventory)),
		buyLimit:  p.SideBudget,
		sellLimit: p.SideBudget,
	}
	result := make([]Target, 0, int(p.QuoteLevels)*2)
	for level := range int(p.QuoteLevels) {
		for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
			price, err := levelPrice(input, level, side)
			if err != nil {
				return nil, errors.Join(ErrInvalidInput, err)
			}
			if price.IsZero() {
				continue
			}
			quantity := allocateQuantity(input, price, side, &funds)
			if quantity.IsZero() {
				continue
			}
			result = append(result, Target{
				Side:        side,
				Level:       level,
				Price:       price,
				Quantity:    quantity,
				TimeInForce: domain.TimeInForcePostOnly,
			})
		}
	}
	return result, nil
}

func validateInput(input Input) error {
	p := input.Parameters
	if err := boundedParameters(p); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	return validateMarketInput(input, p.MaxMarketAge)
}

func validateMarketInput(input Input, maxMarketAge time.Duration) error {
	i := input.Instrument
	if i.ID <= 0 || i.MetadataVersion <= 0 || i.Status != domain.InstrumentStatusTrading || !domain.FixedDecimal(i.PriceTick, 20, 18, false) ||
		!domain.FixedDecimal(i.QuantityStep, 20, 18, false) || !domain.FixedDecimal(i.MinNotional, 20, 18, false) {
		return ErrInvalidInput
	}
	if !domain.FixedDecimal(input.Inventory, 40, 36, true) || !domain.FixedDecimal(input.AvailableBase, 40, 36, true) ||
		!domain.FixedDecimal(input.AvailableQuote, 40, 36, true) || !domain.FixedDecimal(input.QuoteFeeRate, 1, 18, true) || input.QuoteFeeRate.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return ErrInvalidInput
	}
	if !domain.FixedDecimal(input.LocalBid, 20, 18, true) || !domain.FixedDecimal(input.LocalAsk, 20, 18, true) ||
		input.LocalBid.IsPositive() && input.LocalAsk.IsPositive() && !input.LocalBid.LessThan(input.LocalAsk) {
		return ErrInvalidInput
	}
	if input.Market.FeedID <= 0 || input.Market.OwnerID == "" || input.Market.OwnerEpoch <= 0 || input.Market.Revision <= 0 ||
		!input.Market.AuthorityUntil.After(input.Now) || input.Market.Reading.ValidUntil.After(input.Market.AuthorityUntil) {
		return domain.ErrUnreliableMarket
	}
	return input.Market.Reading.Selected(input.Source, i.ID, input.Symbol, input.Mode, input.Now, min(maxMarketAge, 5*time.Second))
}

func boundedParameters(p domain.MakerParameters) error {
	for _, value := range []decimal.Decimal{p.LevelNotional, p.SideBudget, p.InventoryTarget, p.InventorySoftLimit, p.InventoryHardLimit, p.HedgeThreshold, p.MaxHedgeBacklog} {
		if !domain.FixedDecimal(value, 20, 18, true) {
			return ErrInvalidInput
		}
	}
	for _, value := range []decimal.Decimal{p.BaseSpreadBPS, p.LevelSpacingBPS, p.RepriceBPS} {
		if !domain.FixedDecimal(value, 5, 10, true) {
			return ErrInvalidInput
		}
	}
	return nil
}

func levelPrice(input Input, level int, side domain.Side) (decimal.Decimal, error) {
	p := input.Parameters
	imbalance := input.Inventory.Sub(p.InventoryTarget)
	imbalance = maximum(p.InventorySoftLimit.Neg(), minimum(imbalance, p.InventorySoftLimit))
	denominator := p.InventorySoftLimit.Mul(decimal.NewFromInt(20000))
	center := denominator.Sub(p.BaseSpreadBPS.Mul(imbalance))
	offset := p.BaseSpreadBPS.Add(p.LevelSpacingBPS.Mul(decimal.NewFromInt(2 * int64(level)))).Mul(p.InventorySoftLimit)
	if side == domain.SideBuy {
		center = center.Sub(offset)
	} else {
		center = center.Add(offset)
	}
	numerator := input.Market.Reading.Mid.Mul(center)
	if !numerator.IsPositive() {
		return decimal.Zero, nil
	}
	lots, remainder := numerator.QuoRem(denominator.Mul(input.Instrument.PriceTick), 0)
	if side == domain.SideSell && !remainder.IsZero() {
		lots = lots.Add(decimal.NewFromInt(1))
	}
	price := lots.Mul(input.Instrument.PriceTick)
	if side == domain.SideBuy && input.LocalAsk.IsPositive() {
		price = minimum(price, input.LocalAsk.Sub(input.Instrument.PriceTick))
	}
	if side == domain.SideSell && input.LocalBid.IsPositive() {
		price = maximum(price, input.LocalBid.Add(input.Instrument.PriceTick))
	}
	if !price.IsPositive() {
		return decimal.Zero, nil
	}
	price, err := domain.RoundPrice(price, input.Instrument.PriceTick, side)
	if err != nil || !domain.FixedDecimal(price, 20, 18, false) {
		return decimal.Zero, ErrInvalidInput
	}
	return price, nil
}

func allocateQuantity(input Input, price decimal.Decimal, side domain.Side, funds *allocation) decimal.Decimal {
	limit := minimum(input.Parameters.LevelNotional, funds.buyLimit)
	if side == domain.SideSell {
		limit = minimum(input.Parameters.LevelNotional, funds.sellLimit)
	}
	return allocateAt(input, price, side, limit, funds)
}

func allocateAt(input Input, price decimal.Decimal, side domain.Side, limit decimal.Decimal, funds *allocation) decimal.Decimal {
	step := input.Instrument.QuantityStep
	lots, _ := limit.QuoRem(price.Mul(step), 0)
	if side == domain.SideBuy {
		cashLots, _ := funds.quote.QuoRem(price.Mul(step).Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)), 0)
		inventoryLots, _ := funds.buyRoom.QuoRem(step, 0)
		lots = minimum(lots, minimum(cashLots, inventoryLots))
	} else {
		baseLots, _ := funds.base.QuoRem(step, 0)
		lots = minimum(lots, baseLots)
	}
	quantity := lots.Mul(step)
	notional := price.Mul(quantity)
	if !domain.FixedDecimal(quantity, 20, 18, false) || notional.LessThan(input.Instrument.MinNotional) {
		return decimal.Zero
	}
	if side == domain.SideBuy {
		funds.quote = funds.quote.Sub(notional.Mul(decimal.NewFromInt(1).Add(input.QuoteFeeRate)))
		funds.buyRoom = funds.buyRoom.Sub(quantity)
		funds.buyLimit = funds.buyLimit.Sub(notional)
	} else {
		funds.base = funds.base.Sub(quantity)
		funds.sellLimit = funds.sellLimit.Sub(notional)
	}
	return quantity
}

func minimum(a, b decimal.Decimal) decimal.Decimal {
	if a.LessThan(b) {
		return a
	}
	return b
}

func maximum(a, b decimal.Decimal) decimal.Decimal {
	if a.GreaterThan(b) {
		return a
	}
	return b
}
