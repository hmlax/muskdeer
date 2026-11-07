package domain

import (
	"errors"

	"github.com/shopspring/decimal"
)

// MessageGridReferenceOutside identifies an out-of-range grid reference.
const MessageGridReferenceOutside = "selected reference is outside configured grid range"

// ErrGridReferenceOutside protects orders while reference prices leave the grid.
var ErrGridReferenceOutside = errors.New(MessageGridReferenceOutside)

// GridPrice is a fixed arithmetic level, independent of spot or futures funding.
type GridPrice struct {
	Level int
	Side  Side
	Price decimal.Decimal
}

// GridPrices rounds buys down and sells up, skips midpoint/crossing levels and
// never moves an unsafe order to a different price. Funding remains venue-specific.
func GridPrices(p GridParameters, tick, mid, localBid, localAsk decimal.Decimal) ([]GridPrice, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if !tick.IsPositive() || p.UpperPrice.Sub(p.LowerPrice).LessThan(tick.Mul(decimal.NewFromInt(int64(p.GridCount-1)))) {
		return nil, ErrInvalidParameters
	}
	if mid.LessThan(p.LowerPrice) || mid.GreaterThan(p.UpperPrice) {
		return nil, ErrGridReferenceOutside
	}
	divisor := decimal.NewFromInt(int64(p.GridCount - 1))
	var result []GridPrice
	for level := range int(p.GridCount) {
		numerator := p.LowerPrice.Mul(divisor).Add(p.UpperPrice.Sub(p.LowerPrice).Mul(decimal.NewFromInt(int64(level))))
		comparison := numerator.Cmp(mid.Mul(divisor))
		if comparison == 0 {
			continue
		}
		side := SideBuy
		if comparison > 0 {
			side = SideSell
		}
		lots, remainder := numerator.QuoRem(divisor.Mul(tick), 0)
		if side == SideSell && !remainder.IsZero() {
			lots = lots.Add(decimal.NewFromInt(1))
		}
		price := lots.Mul(tick)
		if price.LessThan(p.LowerPrice) || price.GreaterThan(p.UpperPrice) || !FixedDecimal(price, 20, 18, false) || side == SideBuy && localAsk.IsPositive() && !price.LessThan(localAsk) || side == SideSell && localBid.IsPositive() && !price.GreaterThan(localBid) {
			continue
		}
		result = append(result, GridPrice{
			Level: level,
			Side:  side,
			Price: price,
		})
	}
	return result, nil
}
