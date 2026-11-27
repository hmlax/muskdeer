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
	depth := make(map[domain.Side]int)
	seen := make(map[string]bool)
	for _, order := range snapshot.Orders {
		if !retained.orders[order.OrderId] {
			continue
		}
		key, _, err := orderKey(order)
		if err != nil {
			return err
		}
		seen[key] = true
		depth[domain.Side(order.Side)]++
	}
	var candidates []rollingQuote
	for _, order := range snapshot.Orders {
		if retained.orders[order.OrderId] || !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(profile)) {
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
		if seen[key] {
			retained.pending = true
			continue // Duplicate orders do not provide another price level.
		}
		seen[key] = true
		depth[domain.Side(order.Side)]++
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
		batch := max(1, int(decimal.NewFromInt(int64(levels[side])).Mul(replacement).Ceil().IntPart()))
		// A shallow side must accumulate depth before spending it on refreshes.
		// Unsafe and duplicate quotes were excluded and still get cancelled.
		floor := max(1, levels[side]-batch)
		allowance := min(batch, max(0, depth[side]-floor))
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
	missing, refresh := quoteMaintenanceTargets(targets, snapshot)
	return append(missing, refresh...)
}

func (cycle *quoteCycle) quoteMakerMaintenanceOrder() []*commonv1.VenueTarget {
	missing, refresh := quoteMaintenanceTargets(cycle.targets, cycle.snapshot)
	depth := cycle.visibleQuoteDepth()
	if depth == nil {
		depth = make(map[domain.Side]int)
	}
	desired := make(map[domain.Side]int)
	for _, target := range cycle.targets {
		desired[domain.Side(target.Side)]++
	}
	bestFirst := cycle.visibleOuterQuoteCoverage()
	for side, count := range desired {
		bestFirst[side] = bestFirst[side] || depth[side] >= count
	}
	queues := make(map[domain.Side][]*commonv1.VenueTarget)
	for _, target := range quoteCoverageOrder(missing, bestFirst) {
		side := domain.Side(target.Side)
		queues[side] = append(queues[side], target)
	}
	best := quoteBestTargets(cycle.targets)
	touch := make(map[domain.Side]*commonv1.VenueTarget)
	for _, target := range missing {
		side := domain.Side(target.Side)
		if bestFirst[side] && target == best[side] {
			touch[side] = target
		}
	}
	ordered := make([]*commonv1.VenueTarget, 0, len(cycle.targets))
	for len(ordered) < len(missing) {
		side := quoteMaintenanceSide(queues, depth, touch, len(ordered))
		ordered = append(ordered, queues[side][0])
		queues[side] = queues[side][1:]
		// Shallow coverage comes first; the same wave also reserves a missing
		// opposite touch before deeper levels consume the remaining requests.
		depth[side]++
	}
	return append(ordered, refresh...)
}

func quoteBestTargets(targets []*commonv1.VenueTarget) map[domain.Side]*commonv1.VenueTarget {
	best := make(map[domain.Side]*commonv1.VenueTarget)
	prices := make(map[domain.Side]decimal.Decimal)
	for _, target := range targets {
		side := domain.Side(target.Side)
		price, _ := native.Value(target.Price, false, false)
		if best[side] == nil || side == domain.SideBuy && price.GreaterThan(prices[side]) || side == domain.SideSell && price.LessThan(prices[side]) {
			best[side], prices[side] = target, price
		}
	}
	return best
}

func quoteMaintenanceSide(queues map[domain.Side][]*commonv1.VenueTarget, depth map[domain.Side]int, touch map[domain.Side]*commonv1.VenueTarget, selected int) domain.Side {
	const shallowProposalsBeforeOppositeTouch = 2

	// Two proposals cover the shallow side's outer edge and its best price.
	// An outer-covered opposite side need not wait for an entire depth rebuild.
	if selected == shallowProposalsBeforeOppositeTouch {
		for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
			if len(queues[side]) > 0 && queues[side][0] == touch[side] {
				return side
			}
		}
	}
	side := domain.SideBuy
	if len(queues[side]) == 0 || len(queues[domain.SideSell]) > 0 && depth[domain.SideSell] < depth[side] {
		side = domain.SideSell
	}
	return side
}

func quoteMaintenanceTargets(targets []*commonv1.VenueTarget, snapshot *commonv1.VenueSnapshot) ([]*commonv1.VenueTarget, []*commonv1.VenueTarget) {
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
	return missing, refresh
}

// A side without stable outer coverage seeds its outer edge first. Existing
// visible coverage, including a shallow side's far bridge, lets the best price
// lead without spending the first scarce SDK slot on another outer quote.
func quoteCoverageOrder(targets []*commonv1.VenueTarget, bestFirst map[domain.Side]bool) []*commonv1.VenueTarget {
	sides := make([][]*commonv1.VenueTarget, 2)
	for _, target := range targets {
		side := 0
		if target.Side == string(domain.SideSell) {
			side = 1
		}
		sides[side] = append(sides[side], target)
	}
	for side, quotes := range sides {
		sort.SliceStable(quotes, func(i, j int) bool {
			left, _ := native.Value(quotes[i].Price, false, false)
			right, _ := native.Value(quotes[j].Price, false, false)
			if side == 0 {
				return left.GreaterThan(right)
			}
			return left.LessThan(right)
		})
	}
	ordered := make([]*commonv1.VenueTarget, 0, len(targets))
	for level := range max(len(sides[0]), len(sides[1])) {
		for side, quotes := range sides {
			if level >= len(quotes) {
				continue
			}
			kind := domain.SideBuy
			if side == 1 {
				kind = domain.SideSell
			}
			index := level
			if !bestFirst[kind] {
				index = len(quotes) - 1 - level/2
				if level%2 != 0 {
					index = level / 2
				}
			}
			ordered = append(ordered, quotes[index])
		}
	}
	return ordered
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
