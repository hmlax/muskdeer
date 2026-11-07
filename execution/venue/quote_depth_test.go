package venue

import (
	"strconv"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestDepthReplacementWaitsForVisibleReplacementAndPreservesEveryLevel(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"no_replacement", "ack_without_visible_order", "visible_size_replacement", "visible_price_replacement", "unsafe_price", "no_bridge_capacity"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			profile := &common.VenueProfilePolicy{
				Symbol:               "BTCUSDT",
				StrategyInstanceId:   1,
				MaximumOrderNotional: "500",
			}
			snapshot := &common.VenueSnapshot{}
			wanted := make(map[string]*SubmitRequest)
			for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
				for level := range 20 {
					price := decimal.NewFromInt(100).Sub(decimal.NewFromInt(int64(level + 1)).Shift(-1))
					if side == domain.SideSell {
						price = decimal.NewFromInt(100).Add(decimal.NewFromInt(int64(level + 1)).Shift(-1))
					}
					key := string(side) + "/" + price.String()
					wanted[key] = &SubmitRequest{
						Side:     side,
						Price:    price,
						Quantity: decimal.NewFromInt(2),
					}
					snapshot.Orders = append(snapshot.Orders, &common.VenueOrder{
						OrderId:          string(side) + strconv.Itoa(level),
						ClientId:         "deer_BTCUSDT_s1_old_" + string(side) + strconv.Itoa(level),
						Side:             string(side),
						Price:            price.String(),
						Quantity:         "1",
						ExecutedQuantity: "0",
						Status:           native.StatusOpen,
					})
				}
			}
			cycle := &quoteCycle{
				profile:     profile,
				snapshot:    snapshot,
				bridgeSlots: 1,
			}
			wantCancelled := 0
			switch kind {
			case "ack_without_visible_order":
				cycle.admitted = []*common.VenueTarget{{
					Side:     string(domain.SideBuy),
					Price:    "99.9",
					Quantity: "2",
				}}
			case "visible_size_replacement", "visible_price_replacement":
				for _, original := range []*common.VenueOrder{snapshot.Orders[0], snapshot.Orders[20]} {
					next := proto.CloneOf(original)
					next.OrderId += "_replacement"
					next.ClientId += "_replacement"
					next.Quantity = "2"
					if kind == "visible_price_replacement" {
						if next.Side == string(domain.SideBuy) {
							next.Price = "99.95"
							wanted["BUY/99.95"] = &SubmitRequest{
								Side:     domain.SideBuy,
								Price:    decimal.RequireFromString(next.Price),
								Quantity: decimal.NewFromInt(2),
							}
							delete(wanted, "BUY/98")
						} else {
							next.Price = "100.05"
							wanted["SELL/100.05"] = &SubmitRequest{
								Side:     domain.SideSell,
								Price:    decimal.RequireFromString(next.Price),
								Quantity: decimal.NewFromInt(2),
							}
							delete(wanted, "SELL/102")
						}
					}
					// Native order iteration order must not change which quote survives.
					snapshot.Orders = append([]*common.VenueOrder{next}, snapshot.Orders...)
				}
				wantCancelled = 2
			case "unsafe_price":
				snapshot.Orders[0].Price = "101"
				wantCancelled = 1
			case "no_bridge_capacity":
				cycle.bridgeSlots = 0
				wantCancelled = 4
			}
			retained, err := retainQuoteOrders(profile, snapshot, wanted, true, decimal.RequireFromString("0.1"))
			if err != nil {
				t.Fatal(err)
			}
			cycle.retained = retained
			retiring := depthRetirements(cycle, wanted, decimal.RequireFromString("0.1"))
			if len(retiring) != wantCancelled {
				t.Fatalf("depthRetirements(%s) = %d cancellations, want %d", kind, len(retiring), wantCancelled)
			}
			deleted := make(map[string]bool)
			for _, order := range retiring {
				deleted[order.OrderId] = true
			}
			levels := make(map[string]bool)
			for _, order := range snapshot.Orders {
				if !deleted[order.OrderId] {
					levels[order.Side+"/"+order.Price] = true
				}
			}
			wantLevels := 40
			switch kind {
			case "unsafe_price":
				wantLevels = 39
			case "no_bridge_capacity":
				wantLevels = 36
			}
			if len(levels) != wantLevels {
				t.Fatalf("remaining unique levels(%s) = %d, want %d", kind, len(levels), wantLevels)
			}
		})
	}
}
