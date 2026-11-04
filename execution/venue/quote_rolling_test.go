package venue

import (
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestQuoteSizeRefreshKeepsNinetyPercentOfEachSide(t *testing.T) {
	t.Parallel()
	profile := &commonv1.HimalayaProfilePolicy{
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "500",
	}
	snapshot := &commonv1.HimalayaSnapshot{}
	wanted := make(map[string]*SubmitRequest)
	for index, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		for level := range 20 {
			distance := decimal.NewFromInt(int64(level + 1)).Shift(-1)
			price := decimal.NewFromInt(100).Sub(distance)
			if side == domain.SideSell {
				price = decimal.NewFromInt(100).Add(distance)
			}
			order := retentionOrder(strconv.Itoa(index*20+level+1), side, price.String())
			snapshot.Orders = append(snapshot.Orders, order)
			wanted[string(side)+"/"+price.String()] = &SubmitRequest{
				Side:     side,
				Price:    price,
				Quantity: decimal.NewFromInt(2),
			}
		}
	}
	retained, err := retainQuoteOrders(profile, snapshot, wanted, true)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, order := range snapshot.Orders {
		if retained.orders[order.OrderId] {
			counts[order.Side]++
		}
	}
	if counts[string(domain.SideBuy)] != 18 || counts[string(domain.SideSell)] != 18 {
		t.Fatalf("retainQuoteOrders(size refresh) kept buys=%d sells=%d, want 18/18 while two replacements await proof", counts[string(domain.SideBuy)], counts[string(domain.SideSell)])
	}
	if !retained.pending || len(retained.targets) != 36 {
		t.Fatalf("retainQuoteOrders(size refresh) pending=%t occupied=%d, want pending and 36 occupied target prices", retained.pending, len(retained.targets))
	}
}

func TestRollingQuotePlacementUsesLiveOppositePricesAndBoundedCapacity(t *testing.T) {
	t.Parallel()
	target := &commonv1.HimalayaTarget{
		Side:     string(domain.SideBuy),
		Price:    "100",
		Quantity: "1",
	}
	for _, kind := range []string{"available", "equal_ask", "crossed_ask", "terminal_ask", "one_bridge", "at_capacity", "admitted_ask"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle := &quoteCycle{
				targets:  []*commonv1.HimalayaTarget{target},
				snapshot: &commonv1.HimalayaSnapshot{},
			}
			want := true
			switch kind {
			case "equal_ask", "crossed_ask", "terminal_ask":
				order := retentionOrder("1", domain.SideSell, "100")
				want = false
				if kind == "crossed_ask" {
					order.Price = "99.9"
				}
				if kind == "terminal_ask" {
					order.Status = native.StatusCanceled
					want = true
				}
				cycle.snapshot.Orders = append(cycle.snapshot.Orders, order)
			case "one_bridge":
				cycle.snapshot.Orders = append(cycle.snapshot.Orders, retentionOrder("1", domain.SideBuy, "99.9"))
			case "at_capacity":
				cycle.snapshot.Orders = append(cycle.snapshot.Orders, retentionOrder("1", domain.SideBuy, "99.8"))
				cycle.admitted = []*commonv1.HimalayaTarget{target}
				want = false
			case "admitted_ask":
				cycle.admitted = []*commonv1.HimalayaTarget{{
					Side:  string(domain.SideSell),
					Price: "100",
				}}
				want = false
			}
			if got := cycle.canPlace(target); got != want {
				t.Fatalf("quoteCycle.canPlace(%s) = %t, want %t", kind, got, want)
			}
		})
	}
}

func TestRollingDuplicateQuotesRemainPendingUntilCleanupCompletes(t *testing.T) {
	t.Parallel()
	profile := &commonv1.HimalayaProfilePolicy{
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "500",
	}
	snapshot := &commonv1.HimalayaSnapshot{Orders: []*commonv1.HimalayaOrder{
		retentionOrder("1", domain.SideBuy, "100"),
		retentionOrder("2", domain.SideSell, "100.2"),
		retentionOrder("3", domain.SideBuy, "100"),
		retentionOrder("4", domain.SideBuy, "100"),
		retentionOrder("5", domain.SideBuy, "100"),
	}}
	wanted := map[string]*SubmitRequest{
		"BUY/100": {
			Side:     domain.SideBuy,
			Price:    decimal.NewFromInt(100),
			Quantity: decimal.NewFromInt(1),
		},
		"SELL/100.2": {
			Side:     domain.SideSell,
			Price:    decimal.RequireFromString("100.2"),
			Quantity: decimal.NewFromInt(1),
		},
	}
	retained, err := retainQuoteOrders(profile, snapshot, wanted, true)
	if err != nil {
		t.Fatal(err)
	}
	if !retained.pending {
		t.Fatalf("retainQuoteOrders(duplicate cleanup) pending=%t, want pending until every extra order is removed", retained.pending)
	}
}

func TestRollingQuotePlacementFillsMissingPricesBeforeSizeRefreshes(t *testing.T) {
	t.Parallel()
	targets := []*commonv1.HimalayaTarget{
		{Side: string(domain.SideBuy), Price: "100"},
		{Side: string(domain.SideBuy), Price: "99.9"},
		{Side: string(domain.SideSell), Price: "100.1"},
		{Side: string(domain.SideSell), Price: "100.2"},
	}
	snapshot := &commonv1.HimalayaSnapshot{Orders: []*commonv1.HimalayaOrder{
		retentionOrder("1", domain.SideBuy, "100"),
		retentionOrder("2", domain.SideSell, "100.1"),
	}}
	ordered := quoteMaintenanceOrder(targets, snapshot)
	if len(ordered) != 4 || ordered[0] != targets[1] || ordered[1] != targets[3] || ordered[2] != targets[0] || ordered[3] != targets[2] {
		t.Fatalf("quoteMaintenanceOrder = %v, want missing bid/ask before populated price size updates", ordered)
	}
}
