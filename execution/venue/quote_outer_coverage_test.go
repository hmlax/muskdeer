package venue

import (
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestShallowSideWithVisibleOuterBridgeRefreshesBestPriceFirst(t *testing.T) {
	t.Parallel()
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		t.Run(string(side), func(t *testing.T) {
			t.Parallel()
			cycle := fullQuoteTouchCycle(20)
			index, best := 19, "99.9"
			if side == domain.SideSell {
				index, best = 39, "100.1"
			}
			cycle.snapshot.Orders = []*common.VenueOrder{cycle.snapshot.Orders[index]}
			for _, target := range cycle.quoteMakerMaintenanceOrder() {
				if target.Side != string(side) {
					continue
				}
				if target.Price != best {
					t.Fatalf("shallow %s already has a visible outer bridge but dispatches %s before best %s", side, target.Price, best)
				}
				return
			}
			t.Fatal("maintenance omitted the configured side")
		})
	}
}

func TestOuterBridgePriorityRequiresStableOwnSafeCoverage(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"pending", "foreign", "oversized", "fully_executed", "inside_outer_edge", "ack_only"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle := fullQuoteTouchCycle(20)
			order := cycle.snapshot.Orders[39]
			cycle.snapshot.Orders = []*common.VenueOrder{order}
			switch kind {
			case "pending":
				cycle.cancelling = map[string]bool{order.OrderId: true}
			case "foreign":
				order.ClientId = native.ClientPrefix + cycle.profile.Symbol + "_s2_other"
			case "oversized":
				order.Quantity = "10"
			case "fully_executed":
				order.ExecutedQuantity = order.Quantity
			case "inside_outer_edge":
				order.Price = "101"
			case "ack_only":
				cycle.snapshot.Orders = nil
				cycle.admitted = cycle.targets[20:]
			}
			for _, target := range cycle.quoteMakerMaintenanceOrder() {
				if target.Side != string(domain.SideSell) {
					continue
				}
				if target.Price != "102" {
					t.Fatalf("%s counted as stable outer coverage: first ask=%s, want outer 102", kind, target.Price)
				}
				return
			}
			t.Fatal("maintenance omitted the ask side")
		})
	}
}
