package venue

import (
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// A cancellation can outlive the SDK caller. This mark is only a liquidity
// reservation, never an order state, terminal proof or financial reservation.
type quoteCancellationMark struct {
	observedAtMs int64
}

func (r *Runtime) pendingQuoteCancellations(snapshot *common.VenueSnapshot) map[string]bool {
	active := make(map[string]bool)
	for _, order := range snapshot.GetOrders() {
		if native.Active(order.Status) {
			active[order.OrderId] = true
		}
	}
	var pending map[string]bool
	r.quoteCancellations.Range(func(key, value any) bool {
		id, mark := key.(string), value.(*quoteCancellationMark)
		// Only a newer, still fresh SDK observation can disprove visibility.
		// Absence clears a depth hint; it does not release any funds or audit.
		if snapshot.GetValidUntilMs() > time.Now().UnixMilli() && snapshot.GetStartedAtMs() > mark.observedAtMs && !active[id] && r.quoteCancellations.CompareAndDelete(id, mark) {
			return true
		}
		if pending == nil {
			pending = make(map[string]bool)
		}
		pending[id] = true
		return true
	})
	return pending
}

// This projection is used only by retention. The original complete snapshot
// always governs risk, crossing and capacity, including every pending order.
func quoteRetentionSnapshot(snapshot *common.VenueSnapshot, pending map[string]bool) *common.VenueSnapshot {
	if len(pending) == 0 {
		return snapshot
	}
	orders := make([]*common.VenueOrder, 0, len(snapshot.Orders))
	for _, order := range snapshot.Orders {
		if !pending[order.OrderId] {
			orders = append(orders, order)
		}
	}
	return &common.VenueSnapshot{Orders: orders}
}
