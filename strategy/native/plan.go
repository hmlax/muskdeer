// Package native owns exact, selected-source SDK maker targets in the strategy process.
package native

import (
	"sort"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
)

type target struct {
	side     domain.Side
	price    decimal.Decimal
	quantity decimal.Decimal
}

// BuildTargets preserves the requested fraction, clips finite per-order risk,
// applies deterministic minimum lot/notional and merges identical native ticks.
func BuildTargets(cfg *commonv1.HimalayaPolicy, profile *commonv1.HimalayaProfilePolicy, book *commonv1.HimalayaBook, snapshot *commonv1.HimalayaSnapshot) ([]*commonv1.HimalayaTarget, error) {
	if book.GetSource() != values.Source || book.GetSegment() != values.Segment || book.GetSymbol() != profile.Symbol ||
		snapshot.GetScope().GetSymbol() != profile.Symbol || snapshot.Scope.UserId != profile.UserId || snapshot.Scope.FundId != profile.FundId ||
		profile.PriceTick != snapshot.GetMarket().GetPriceTick() || profile.QuantityStep != snapshot.Market.QuantityStep || profile.Leverage > snapshot.Market.MaximumLeverage {
		return nil, values.ErrInvalid
	}
	tick, e1 := values.Value(profile.PriceTick, false, false)
	step, e2 := values.Value(profile.QuantityStep, false, false)
	fraction, e3 := values.Value(cfg.QuantityFraction, false, false)
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, values.ErrInvalid
	}
	grouped := make(map[string]*target)
	for index, rows := range [][]*commonv1.HimalayaLevel{book.Bids, book.Asks} {
		side := domain.SideBuy
		if index == 1 {
			side = domain.SideSell
		}
		for level, row := range rows {
			if level >= int(cfg.Depth) {
				break
			}
			price, e1 := values.Value(row.Price, false, false)
			quantity, e2 := values.Value(row.Quantity, false, false)
			if e1 != nil || e2 != nil {
				return nil, values.ErrInvalid
			}
			price, err := domain.RoundPrice(price, tick, side)
			if err != nil || !price.IsPositive() {
				return nil, values.ErrInvalid
			}
			quantity, err = domain.RoundQuantity(quantity.Mul(fraction), step)
			if err != nil {
				return nil, err
			}
			key := string(side) + "/" + price.String()
			if previous := grouped[key]; previous != nil {
				previous.quantity = previous.quantity.Add(quantity)
			} else {
				grouped[key] = &target{
					side:     side,
					price:    price,
					quantity: quantity,
				}
			}
		}
	}
	var ordered []*target
	for _, candidate := range grouped {
		quantity, err := constrainedQuantity(candidate, snapshot.Market, profile.MaximumOrderNotional, step)
		if err != nil {
			return nil, err
		}
		candidate.quantity = quantity
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].side != ordered[j].side {
			return ordered[i].side == domain.SideBuy
		}
		if ordered[i].side == domain.SideBuy {
			return ordered[i].price.GreaterThan(ordered[j].price)
		}
		return ordered[i].price.LessThan(ordered[j].price)
	})
	var result []*commonv1.HimalayaTarget
	for _, candidate := range ordered {
		result = append(result, &commonv1.HimalayaTarget{
			Side:     string(candidate.side),
			Price:    candidate.price.String(),
			Quantity: candidate.quantity.String(),
		})
	}
	if len(result) == 0 {
		return nil, values.ErrInvalid
	}
	return result, nil
}

func constrainedQuantity(candidate *target, market *commonv1.HimalayaMarket, capText string, step decimal.Decimal) (decimal.Decimal, error) {
	minimum, e1 := values.Value(market.MinimumQuantity, false, true)
	notional, e2 := values.Value(market.MinimumNotional, false, true)
	maximum, e3 := values.Value(market.MaximumQuantity, false, true)
	limit, e4 := domain.ParseLedgerDecimal(capText, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return decimal.Zero, values.ErrInvalid
	}
	minimumLots, remainder := minimum.QuoRem(step, 0)
	if !remainder.IsZero() {
		minimumLots = minimumLots.Add(decimal.NewFromInt(1))
	}
	minimum = minimumLots.Mul(step)
	notionalLots, remainder := notional.QuoRem(candidate.price.Mul(step), 0)
	if !remainder.IsZero() {
		notionalLots = notionalLots.Add(decimal.NewFromInt(1))
	}
	minimum = decimal.Max(step, minimum, notionalLots.Mul(step))
	quantity := decimal.Max(candidate.quantity, minimum)
	capLots, _ := limit.QuoRem(candidate.price.Mul(step), 0)
	quantity = decimal.Min(quantity, capLots.Mul(step))
	if maximum.IsPositive() {
		quantity = decimal.Min(quantity, maximum)
	}
	quantity, err := domain.RoundQuantity(quantity, step)
	if err != nil || quantity.LessThan(minimum) {
		return decimal.Zero, values.ErrInvalid
	}
	return quantity, nil
}
