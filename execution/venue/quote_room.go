package venue

import (
	"context"
	"strings"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// A preceding bridge may occupy the last slot when the source moves again.
// Retire a small unsafe or still-visible pending subset, OBSERVE the freed slots,
// then run fill-before-drain. Never assume an acknowledgement freed capacity.
func (r *Runtime) makeQuoteRoom(ctx context.Context, cycle *quoteCycle, wanted map[string]*SubmitRequest) error {
	if !r.directMaker() {
		return nil
	}
	orders, err := quoteRoomRetirements(cycle, wanted)
	if err != nil || len(orders) == 0 {
		return err
	}
	if err := r.cancelOrders(ctx, cycle.lease, cycle.profile, orders, cycle.result); err != nil {
		return err
	}
	snapshot, err := r.refresh(ctx, cycle.profile, cycle.lease, cycle.authority.Book)
	if err != nil {
		return err
	}
	cycle.cancelling = r.pendingQuoteCancellations(snapshot)
	retained, err := retainQuoteOrders(cycle.profile, quoteRetentionSnapshot(snapshot, cycle.cancelling), wanted, true, r.cfg.ReplacementFraction)
	if err != nil {
		return err
	}
	cycle.snapshot, cycle.retained = snapshot, retained
	return nil
}

func quoteRoomRetirements(cycle *quoteCycle, wanted map[string]*SubmitRequest) ([]*common.VenueOrder, error) {
	levels, best := quoteTargetDepths(wanted)
	limit, err := native.Value(cycle.profile.MaximumOrderNotional, false, false)
	if err != nil {
		return nil, err
	}
	active := make(map[domain.Side]int)
	stable := make(map[domain.Side]int)
	for _, order := range cycle.snapshot.Orders {
		if native.Active(order.Status) {
			active[domain.Side(order.Side)]++
			if !cycle.cancelling[order.OrderId] && strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
				stable[domain.Side(order.Side)]++
			}
		}
	}
	selected := make(map[domain.Side]int)
	retiring := make(map[domain.Side]int)
	var orders []*common.VenueOrder
	for _, order := range cycle.snapshot.Orders {
		side := domain.Side(order.Side)
		if !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) ||
			levels[side] == 0 || active[side] < levels[side]+int(cycle.bridgeSlots) ||
			selected[side] >= min(maximumQuoteWave/2, active[side]-1) {
			continue
		}
		if _, safe := bridgePrice(order, best, limit); safe && !cycle.cancelling[order.OrderId] {
			continue
		}
		if !cycle.cancelling[order.OrderId] && stable[side]-retiring[side] <= 1 {
			continue
		}
		orders = append(orders, order)
		selected[side]++
		if !cycle.cancelling[order.OrderId] {
			retiring[side]++
		}
	}
	return orders, nil
}
