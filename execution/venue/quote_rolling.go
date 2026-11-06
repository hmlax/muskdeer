package venue

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type rollingQuote struct {
	order *commonv1.VenueOrder
	key   string
	price decimal.Decimal
}

// The database policy bounds the fraction of safe obsolete levels retired per side.
// Unsafe prices, oversized orders and legacy quotes still require cancellation.
func retainRollingOrders(profile *commonv1.VenueProfilePolicy, snapshot *commonv1.VenueSnapshot, wanted map[string]*SubmitRequest, retained *quoteRetention, replacement decimal.Decimal) error {
	limit, err := native.Value(profile.MaximumOrderNotional, false, false)
	if err != nil {
		return err
	}
	levels, best := quoteTargetDepths(wanted)
	var candidates []rollingQuote
	for _, order := range snapshot.Orders {
		if retained.orders[order.OrderId] || !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.ClientPrefix+profile.Symbol+"_") {
			continue
		}
		price, safe := bridgePrice(order, best, limit)
		if !safe {
			continue
		}
		key, _, err := orderKey(order)
		if err != nil {
			return err
		}
		candidates = append(candidates, rollingQuote{
			order: order,
			key:   key,
			price: price,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return rollingQuoteLess(candidates[i], candidates[j], wanted)
	})
	retiring := make(map[domain.Side]int)
	for _, candidate := range candidates {
		side := domain.Side(candidate.order.Side)
		allowance := max(1, int(decimal.NewFromInt(int64(levels[side])).Mul(replacement).Ceil().IntPart()))
		if retiring[side] < allowance {
			retiring[side]++
			continue
		}
		retained.orders[candidate.order.OrderId] = true
	}
	return nil
}

func quoteTargetDepths(wanted map[string]*SubmitRequest) (map[domain.Side]int, map[domain.Side]decimal.Decimal) {
	levels := make(map[domain.Side]int)
	best := make(map[domain.Side]decimal.Decimal)
	for _, target := range wanted {
		levels[target.Side]++
		previous := best[target.Side]
		if previous.IsZero() || target.Side == domain.SideBuy && target.Price.GreaterThan(previous) || target.Side == domain.SideSell && target.Price.LessThan(previous) {
			best[target.Side] = target.Price
		}
	}
	return levels, best
}

func rollingQuoteLess(left, right rollingQuote, wanted map[string]*SubmitRequest) bool {
	if left.order.Side != right.order.Side {
		return left.order.Side == string(domain.SideBuy)
	}
	if (wanted[left.key] == nil) != (wanted[right.key] == nil) {
		return wanted[left.key] == nil
	}
	if left.order.Side == string(domain.SideBuy) {
		return left.price.GreaterThan(right.price)
	}
	return left.price.LessThan(right.price)
}

func quoteMaintenanceOrder(targets []*commonv1.VenueTarget, snapshot *commonv1.VenueSnapshot) []*commonv1.VenueTarget {
	present := make(map[string]bool)
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		if key, _, err := orderKey(order); err == nil {
			present[key] = true
		}
	}
	missing := make([]*commonv1.VenueTarget, 0, len(targets))
	refresh := make([]*commonv1.VenueTarget, 0, len(targets))
	for _, target := range quoteSubmissionOrder(targets) {
		price, _ := native.Value(target.Price, false, false) // Already validated by targets.
		if present[target.Side+"/"+price.String()] {
			refresh = append(refresh, target)
		} else {
			missing = append(missing, target)
		}
	}
	return append(missing, refresh...)
}

func (cycle *quoteCycle) canPlace(target *commonv1.VenueTarget) bool {
	side := domain.Side(target.Side)
	price, err := native.Value(target.Price, false, false)
	if err != nil || side != domain.SideBuy && side != domain.SideSell {
		return false
	}
	capacity := int(cycle.bridgeSlots)
	for _, proposal := range cycle.targets {
		if proposal.Side == target.Side {
			capacity++
		}
	}
	active := 0
	for _, order := range cycle.snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		if order.Side == target.Side {
			active++
		}
		if quoteBlocksPlacement(side, price, domain.Side(order.Side), order.Price) {
			return false
		}
	}
	for _, admitted := range cycle.admitted {
		if admitted.Side == target.Side {
			active++
		}
		if quoteBlocksPlacement(side, price, domain.Side(admitted.Side), admitted.Price) {
			return false
		}
	}
	return active < capacity
}

func quoteBlocksPlacement(side domain.Side, price decimal.Decimal, otherSide domain.Side, otherText string) bool {
	if otherSide == side {
		return false
	}
	other, err := native.Value(otherText, false, false)
	if err != nil {
		return true
	}
	return side == domain.SideBuy && !price.LessThan(other) || side == domain.SideSell && !price.GreaterThan(other)
}
