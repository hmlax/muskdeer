package venue

import (
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

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
