package venue

import (
	"strings"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// A full safe bridge can otherwise starve a better touch indefinitely: source
// updates defer ordinary retirement while every placement is at capacity. Retire
// at most one far obsolete order, preserving the closest quote and safe depth.
// makeQuoteRoom still observes the cancellation before using any freed capacity.
func quoteTouchRoomOrder(cycle *quoteCycle, wanted map[string]*SubmitRequest, side domain.Side, desired int, best map[domain.Side]decimal.Decimal, limit decimal.Decimal) *common.VenueOrder {
	counts := make(map[string]int)
	var closest decimal.Decimal
	for _, order := range cycle.snapshot.Orders {
		if !stableOwnQuote(cycle, order, side) {
			continue
		}
		price, safe := bridgePrice(order, best, limit)
		if !safe {
			continue
		}
		counts[price.String()]++
		if closest.IsZero() || side == domain.SideBuy && price.GreaterThan(closest) || side == domain.SideSell && price.LessThan(closest) {
			closest = price
		}
	}
	if closest.IsZero() || side == domain.SideBuy && !best[side].GreaterThan(closest) || side == domain.SideSell && !best[side].LessThan(closest) {
		return nil // An equal/better visible touch needs no room for a size refresh.
	}
	floor := min(desired, len(counts))
	var candidate *common.VenueOrder
	var farthest decimal.Decimal
	for _, order := range cycle.snapshot.Orders {
		if !stableOwnQuote(cycle, order, side) {
			continue
		}
		price, safe := bridgePrice(order, best, limit)
		if !safe {
			continue
		}
		key := string(side) + "/" + price.String()
		if counts[price.String()] == 1 && (wanted[key] != nil || price.Equal(closest) || len(counts) <= floor) {
			continue // Preserve configured unique levels, the touch and the depth floor.
		}
		if candidate == nil || side == domain.SideBuy && price.LessThan(farthest) || side == domain.SideSell && price.GreaterThan(farthest) {
			candidate, farthest = order, price
		}
	}
	return candidate
}

func stableOwnQuote(cycle *quoteCycle, order *common.VenueOrder, side domain.Side) bool {
	return order != nil && native.Active(order.Status) && order.Side == string(side) &&
		!cycle.cancelling[order.OrderId] && strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile))
}
