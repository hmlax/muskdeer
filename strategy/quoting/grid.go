package quoting

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// MessageGridOutsideRange is a safe protection reason without market payloads.
const MessageGridOutsideRange = domain.MessageGridReferenceOutside

// ErrGridOutsideRange requires protection until the selected reference returns.
var ErrGridOutsideRange = domain.ErrGridReferenceOutside

// GridInput describes a funded spot grid using one explicit market reference.
type GridInput struct {
	Instrument     Instrument
	Source         domain.MarketSource
	Symbol         string
	Mode           domain.MarketMode
	Market         domain.MarketReference
	Parameters     domain.GridParameters
	Inventory      decimal.Decimal
	AvailableBase  decimal.Decimal
	AvailableQuote decimal.Decimal
	QuoteFeeRate   decimal.Decimal
	LocalBid       decimal.Decimal
	LocalAsk       decimal.Decimal
	Now            time.Time
}

// Grid returns fixed, arithmetic levels: buys below the selected midpoint and
// sells above it. The midpoint level is omitted. Unsafe local-book crossings
// are skipped, never shifted into another grid level. Orders and fills remain
// authoritative execution observations; touching a price creates no fill.
func Grid(input GridInput) ([]Target, error) {
	common := gridMarketInput(input)
	if err := validateGrid(input, common); err != nil {
		return nil, errors.Join(ErrInvalidInput, err)
	}
	p := input.Parameters
	mid := input.Market.Reading.Mid
	if mid.LessThan(p.LowerPrice) || mid.GreaterThan(p.UpperPrice) {
		return nil, ErrGridOutsideRange
	}
	funds := allocation{
		quote:     input.AvailableQuote,
		base:      decimal.Min(input.AvailableBase, input.Inventory),
		buyRoom:   decimal.Max(decimal.Zero, p.InventoryLimit.Sub(input.Inventory)),
		buyLimit:  p.TotalBudget,
		sellLimit: p.TotalBudget,
	}
	remaining := p.TotalBudget
	result := make([]Target, 0, int(p.GridCount))
	prices, err := domain.GridPrices(p, input.Instrument.PriceTick, mid, input.LocalBid, input.LocalAsk)
	if err != nil {
		return nil, err
	}
	for _, level := range prices {
		price, side := level.Price, level.Side
		quantity := allocateAt(common, price, side, decimal.Min(p.PerOrderNotional, remaining), &funds)
		if quantity.IsZero() {
			continue
		}
		remaining = remaining.Sub(price.Mul(quantity))
		result = append(result, Target{
			Side:        side,
			Level:       level.Level,
			Price:       price,
			Quantity:    quantity,
			TimeInForce: domain.TimeInForcePostOnly,
		})
	}
	return result, nil
}

func validateGrid(input GridInput, common Input) error {
	p := input.Parameters
	for _, value := range []decimal.Decimal{
		p.LowerPrice,
		p.UpperPrice,
		p.TotalBudget,
		p.PerOrderNotional,
		p.InventoryLimit,
	} {
		if !domain.FixedDecimal(value, 20, 18, false) {
			return ErrInvalidInput
		}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := validateMarketInput(common, p.MaxMarketAge); err != nil {
		return err
	}
	if p.UpperPrice.Sub(p.LowerPrice).LessThan(input.Instrument.PriceTick.Mul(decimal.NewFromInt(int64(p.GridCount - 1)))) {
		return ErrInvalidInput
	}
	return nil
}

func gridMarketInput(input GridInput) Input {
	return Input{
		Instrument:     input.Instrument,
		Source:         input.Source,
		Symbol:         input.Symbol,
		Mode:           input.Mode,
		Market:         input.Market,
		Inventory:      input.Inventory,
		AvailableBase:  input.AvailableBase,
		AvailableQuote: input.AvailableQuote,
		QuoteFeeRate:   input.QuoteFeeRate,
		LocalBid:       input.LocalBid,
		LocalAsk:       input.LocalAsk,
		Now:            input.Now,
	}
}
