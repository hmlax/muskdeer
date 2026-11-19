package venue

import (
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestFullQuoteSideRefreshesBestPriceBeforeItsOuterEdge(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"full", "shallow_ask", "duplicate_bid", "foreign_bid", "unsafe_bid", "ack_only_bid"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle := fullQuoteTouchCycle()
			wantBid, wantAsk := "99.9", "100.1"
			switch kind {
			case "shallow_ask":
				cycle.snapshot.Orders = cycle.snapshot.Orders[:4]
				wantAsk = "100.3"
			case "duplicate_bid":
				for _, order := range cycle.snapshot.Orders[:3] {
					order.Price = "98.9"
				}
				wantBid = "99.7"
			case "foreign_bid":
				for _, order := range cycle.snapshot.Orders[:3] {
					order.ClientId = "other_instance"
				}
				wantBid = "99.7"
			case "unsafe_bid":
				for _, order := range cycle.snapshot.Orders[:3] {
					order.Price = "100.2"
				}
				wantBid = "99.7"
			case "ack_only_bid":
				cycle.snapshot.Orders = cycle.snapshot.Orders[3:]
				cycle.admitted = cycle.targets[:3]
				wantBid = "99.7"
			}
			first := make(map[string]string)
			seen := make(map[*common.VenueTarget]bool)
			ordered := cycle.quoteMakerMaintenanceOrder()
			for _, target := range ordered {
				if first[target.Side] == "" {
					first[target.Side] = target.Price
				}
				if seen[target] {
					t.Fatal("maintenance duplicated a configured proposal")
				}
				seen[target] = true
			}
			if len(seen) != len(cycle.targets) || first[string(domain.SideBuy)] != wantBid || first[string(domain.SideSell)] != wantAsk {
				t.Fatalf("maintenance first prices = %v with %d proposals, want bid %s ask %s and %d proposals", first, len(seen), wantBid, wantAsk, len(cycle.targets))
			}
			if kind == "shallow_ask" && ordered[0].Side != string(domain.SideSell) {
				t.Fatal("full bid refresh ran before shallow ask refill")
			}
		})
	}
}

func fullQuoteTouchCycle() *quoteCycle {
	cycle := &quoteCycle{
		profile: &common.VenueProfilePolicy{
			Symbol:               "BTCUSDT",
			StrategyInstanceId:   1,
			MaximumOrderNotional: "500",
		},
		bridgeSlots: 1,
		snapshot:    &common.VenueSnapshot{},
	}
	for sideIndex, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		for level := range 3 {
			distance := decimal.NewFromInt(int64(level + 1)).Shift(-1)
			price, old := decimal.NewFromInt(100).Sub(distance), decimal.NewFromInt(99).Sub(distance)
			if side == domain.SideSell {
				price, old = decimal.NewFromInt(100).Add(distance), decimal.NewFromInt(101).Add(distance)
			}
			cycle.targets = append(cycle.targets, &common.VenueTarget{
				Side:     string(side),
				Price:    price.String(),
				Quantity: "0.1",
			})
			cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
				OrderId:          strconv.Itoa(sideIndex*3 + level + 1),
				ClientId:         native.InstanceClientPrefix(cycle.profile) + strconv.Itoa(sideIndex*3+level),
				Side:             string(side),
				Price:            old.String(),
				Quantity:         "0.1",
				ExecutedQuantity: "0",
				Status:           native.StatusOpen,
			})
		}
	}
	return cycle
}
