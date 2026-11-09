package venue

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Fill bridge capacity first, then read the venue before retiring old quotes.
// A send acknowledgement alone cannot stand in for a visible replacement.
func (r *Runtime) maintainCurrentDepth(ctx context.Context, cycle *quoteCycle, wanted map[string]*SubmitRequest, cutover bool) (*execution.QuoteResult, error) {
	if !cutover {
		if err := r.cancelObsolete(ctx, cycle.profile, cycle.lease, cycle.snapshot, cycle.retained, cycle.result, false); err != nil {
			return nil, err
		}
		if cycle.result.Cancelled > 0 {
			return cycle.result, nil
		}
		if r.directMaker() {
			r.cutover.Store(true)
		} else {
			if err := r.store.CompleteCutover(ctx, cycle.lease); err != nil {
				return nil, err
			}
		}
	}
	for _, target := range cycle.targets {
		price, _ := native.Value(target.Price, false, false)
		if cycle.retained.targets[target.Side+"/"+price.String()] {
			continue
		}
		if !cycle.canPlace(target) {
			cycle.retained.pending = true
			continue
		}
		deadline, bounded := ctx.Deadline()
		remaining := time.Until(time.UnixMilli(cycle.authority.Book.ValidUntilMs))
		if !bounded || min(remaining, time.Until(deadline)) <= minimumLiveSDKWindow+50*time.Millisecond {
			cycle.retained.pending = true
			break
		}
		if err := r.submitQuote(ctx, cycle, target); err != nil {
			return nil, err
		}
	}
	if cycle.result.Placed > 0 {
		snapshot, err := r.refresh(ctx, cycle.profile, cycle.lease, cycle.authority.Book)
		if err != nil {
			return nil, err
		}
		cycle.snapshot, cycle.admitted = snapshot, nil
		retained, err := retainQuoteOrders(cycle.profile, snapshot, wanted, true, r.cfg.ReplacementFraction)
		if err != nil {
			return nil, err
		}
		cycle.retained = retained
	}
	for key := range wanted {
		if !cycle.retained.targets[key] {
			cycle.retained.pending = true
			break
		}
	}
	for _, order := range depthRetirements(cycle, wanted, r.cfg.ReplacementFraction) {
		if err := r.cancelOne(ctx, cycle.lease, cycle.profile, order); err != nil {
			return nil, err
		}
		cycle.result.Cancelled++
	}
	if !cycle.retained.pending {
		cycle.result.State = native.StateRunning
	}
	if err := r.logger.LogAttrs(ctx, slog.LevelInfo, cycle.result.State, slog.String("symbol", cycle.profile.Symbol), slog.Uint64("placed", uint64(cycle.result.Placed)), slog.Uint64("cancelled", uint64(cycle.result.Cancelled))); err != nil {
		return nil, err
	}
	return cycle.result, nil
}

// Schedule against the worst case in which every selected cancellation succeeds.
// Only venue-visible own orders contribute to the depth being protected.
func depthRetirements(cycle *quoteCycle, wanted map[string]*SubmitRequest, replacement decimal.Decimal) []*common.VenueOrder {
	levels, best := quoteTargetDepths(wanted)
	limit := decimal.RequireFromString(cycle.profile.MaximumOrderNotional)
	counts, depth := make(map[string]int), make(map[domain.Side]int)
	for _, order := range cycle.snapshot.Orders {
		if !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
			continue
		}
		key, _, err := orderKey(order)
		if err != nil {
			continue // The normalized snapshot was already validated on admission.
		}
		if counts[key] == 0 {
			depth[domain.Side(order.Side)]++
		}
		counts[key]++
	}
	var selected []*common.VenueOrder
	for _, order := range cycle.snapshot.Orders {
		if !native.Active(order.Status) || cycle.retained.orders[order.OrderId] || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
			continue
		}
		key, _, err := orderKey(order)
		if err != nil {
			continue
		}
		side := domain.Side(order.Side)
		floor := levels[side]
		if cycle.bridgeSlots == 0 {
			floor = max(1, floor-max(1, int(decimal.NewFromInt(int64(floor)).Mul(replacement).Ceil().IntPart())))
		}
		_, safe := bridgePrice(order, best, limit)
		if safe && counts[key] == 1 && depth[side] <= floor {
			continue
		}
		selected = append(selected, order)
		counts[key]--
		if counts[key] == 0 {
			depth[side]--
		}
	}
	return selected
}
