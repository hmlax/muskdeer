package venue

import (
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestMissingQuoteRefillCoversBothPriceEdgesBeforeSizeRefresh(t *testing.T) {
	t.Parallel()
	var targets []*common.VenueTarget
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		for level := range 20 {
			price := decimal.NewFromInt(100).Sub(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			if side == domain.SideSell {
				price = decimal.NewFromInt(100).Add(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			}
			targets = append(targets, &common.VenueTarget{
				Side:     string(side),
				Price:    price.String(),
				Quantity: "0.1",
			})
		}
	}
	cycle := &quoteCycle{
		profile:  &common.VenueProfilePolicy{MaximumOrderNotional: "500"},
		targets:  targets,
		snapshot: &common.VenueSnapshot{},
	}
	ordered := cycle.quoteMakerMaintenanceOrder()
	for index, want := range []string{"98", "102", "99.9", "100.1"} {
		if ordered[index].Price != want {
			t.Fatalf("refill[%d] = %s, want %s: first wave must cover both the outer edge and best price", index, ordered[index].Price, want)
		}
	}
	seen := make(map[*common.VenueTarget]bool)
	for _, target := range ordered {
		if seen[target] {
			t.Fatal("refill duplicated a proposal")
		}
		seen[target] = true
	}
	if len(seen) != len(targets) {
		t.Fatal("refill omitted a configured price level")
	}
	// The last replacement in a full side has only one bridge slot. A near-only
	// seed is already crossed by this next frame, while an outer seed survives.
	best := map[domain.Side]decimal.Decimal{
		domain.SideBuy:  decimal.RequireFromString("100.5"),
		domain.SideSell: decimal.RequireFromString("100.6"),
	}
	ask := &common.VenueOrder{
		Side:             ordered[1].Side,
		Price:            ordered[1].Price,
		Quantity:         ordered[1].Quantity,
		ExecutedQuantity: "0",
	}
	if _, safe := bridgePrice(ask, best, decimal.NewFromInt(100)); !safe {
		t.Fatal("first ask replacement was lost before deeper refill could begin")
	}
}

func TestRefillUsesFirstWaveForShallowSideBeforeFullSidePriceRefresh(t *testing.T) {
	t.Parallel()
	cycle := &quoteCycle{
		profile: &common.VenueProfilePolicy{
			Symbol:               "BTCUSDT",
			StrategyInstanceId:   1,
			MaximumOrderNotional: "500",
		},
		snapshot: &common.VenueSnapshot{},
	}
	for level := range 20 {
		for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
			price := decimal.NewFromInt(100).Sub(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			if side == domain.SideSell {
				price = decimal.NewFromInt(100).Add(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			}
			cycle.targets = append(cycle.targets, &common.VenueTarget{
				Side:     string(side),
				Price:    price.String(),
				Quantity: "0.1",
			})
		}
		cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
			OrderId:          strconv.Itoa(level + 1),
			ClientId:         native.InstanceClientPrefix(cycle.profile) + strconv.Itoa(level),
			Side:             string(domain.SideBuy),
			Price:            decimal.NewFromInt(97).Sub(decimal.NewFromInt(int64(level)).Shift(-1)).String(),
			Quantity:         "0.1",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
	}
	cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
		OrderId:          "100",
		ClientId:         native.InstanceClientPrefix(cycle.profile) + "last_ask",
		Side:             string(domain.SideSell),
		Price:            "103",
		Quantity:         "0.1",
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	})
	// Its current best bid is already covered; deeper bid refreshes remain
	// optional while the ask side has only one safe level.
	cycle.snapshot.Orders[0].Price = cycle.targets[0].Price
	ordered := cycle.quoteMakerMaintenanceOrder()
	for _, target := range ordered[:4] {
		if target.Side != string(domain.SideSell) {
			t.Fatal("first refill wave spent capacity refreshing a full bid side while asks were shallow")
		}
	}
}

func TestRefillPriorityRequiresVisibleOwnUniqueSafeDepth(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"full", "duplicate", "foreign", "unsafe", "ack_only"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle := &quoteCycle{
				profile: &common.VenueProfilePolicy{
					Symbol:               "BTCUSDT",
					StrategyInstanceId:   1,
					MaximumOrderNotional: "500",
				},
				snapshot: &common.VenueSnapshot{},
			}
			for index, price := range []string{"99.9", "99.8", "100.1", "100.2"} {
				side := domain.SideBuy
				if index >= 2 {
					side = domain.SideSell
				}
				target := &common.VenueTarget{
					Side:     string(side),
					Price:    price,
					Quantity: "1",
				}
				cycle.targets = append(cycle.targets, target)
				cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
					OrderId:          strconv.Itoa(index + 1),
					ClientId:         native.InstanceClientPrefix(cycle.profile) + strconv.Itoa(index),
					Side:             target.Side,
					Price:            target.Price,
					Quantity:         target.Quantity,
					ExecutedQuantity: "0",
					Status:           native.StatusOpen,
				})
			}
			switch kind {
			case "duplicate":
				cycle.snapshot.Orders[1].Price = "99.9"
			case "foreign":
				cycle.snapshot.Orders[1].ClientId = "other_instance"
			case "unsafe":
				cycle.snapshot.Orders[1].Price = "100.2"
			case "ack_only":
				cycle.snapshot.Orders = cycle.snapshot.Orders[:3]
				cycle.admitted = cycle.targets[3:]
			}
			if got := cycle.needsQuoteDepth(); got != (kind != "full") {
				t.Fatalf("needsQuoteDepth(%s)=%t", kind, got)
			}
		})
	}
}
