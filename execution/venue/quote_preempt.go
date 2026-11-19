package venue

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Deferring an obsolete plan must not retain a quote that is unsafe against
// the accepted replacement. Such a transition runs the existing protection.
func (r *Runtime) deferQuoteRetirement(cycle *quoteCycle) (bool, error) {
	if !r.quotePriceMoved(cycle) {
		return false, nil
	}
	if cycle.snapshot.GetValidUntilMs() <= time.Now().UnixMilli() {
		return false, native.ErrInvalid
	}
	next := r.newerMakerIntent(cycle.plan)
	if next == nil {
		return false, native.ErrInvalid
	}
	best := make(map[domain.Side]decimal.Decimal)
	for side, target := range quoteBestTargets(next.Targets) {
		price, err := native.Value(target.Price, false, false)
		if err != nil {
			return false, err
		}
		best[side] = price
	}
	limit, err := native.Value(cycle.profile.MaximumOrderNotional, false, false)
	if err != nil {
		return false, err
	}
	for _, order := range cycle.snapshot.Orders {
		if !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
			continue
		}
		if _, safe := bridgePrice(order, best, limit); !safe {
			return false, native.ErrInvalid
		}
	}
	return true, nil
}

func (r *Runtime) quotePriceMoved(cycle *quoteCycle) bool {
	if cycle.plan == nil {
		return false
	}
	next := r.newerMakerIntent(cycle.plan)
	if next == nil {
		return false
	}
	previous, latest := quoteBestTargets(cycle.plan.Targets), quoteBestTargets(next.Targets)
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		left, right := previous[side], latest[side]
		if left == nil || right == nil {
			if left != right {
				return true
			}
			continue
		}
		before, e1 := native.Value(left.Price, false, false)
		after, e2 := native.Value(right.Price, false, false)
		if e1 == nil && e2 == nil && !before.Equal(after) {
			return true
		}
	}
	return false
}
