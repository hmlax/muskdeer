package venue

import (
	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Only a visible, stable own quote at/beyond the configured outer edge can
// replace outer-first seeding. A submitted or cancelling quote is not coverage.
func (cycle *quoteCycle) visibleOuterQuoteCoverage() map[domain.Side]bool {
	covered := make(map[domain.Side]bool)
	outer, best := make(map[domain.Side]decimal.Decimal), make(map[domain.Side]decimal.Decimal)
	for _, target := range cycle.targets {
		side := domain.Side(target.Side)
		price, _ := native.Value(target.Price, false, false) // targets already validated.
		if outer[side].IsZero() || side == domain.SideBuy && price.LessThan(outer[side]) || side == domain.SideSell && price.GreaterThan(outer[side]) {
			outer[side] = price
		}
		if best[side].IsZero() || side == domain.SideBuy && price.GreaterThan(best[side]) || side == domain.SideSell && price.LessThan(best[side]) {
			best[side] = price
		}
	}
	limit, err := native.Value(cycle.profile.GetMaximumOrderNotional(), false, false)
	if err != nil {
		return covered
	}
	for _, order := range cycle.snapshot.GetOrders() {
		side := domain.Side(order.GetSide())
		if !stableOwnQuote(cycle, order, side) || outer[side].IsZero() {
			continue
		}
		price, safe := bridgePrice(order, best, limit)
		if safe && (side == domain.SideBuy && !price.GreaterThan(outer[side]) || side == domain.SideSell && !price.LessThan(outer[side])) {
			covered[side] = true
		}
	}
	return covered
}
