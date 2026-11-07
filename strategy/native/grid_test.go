package native

import (
	"errors"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/shopspring/decimal"
)

func TestNativeGridKeepsFixedPricesAndClipsBothFuturesInventoryDirections(t *testing.T) {
	profile := &common.VenueProfilePolicy{
		UserId:               1,
		FundId:               1,
		Symbol:               "BTCUSDT",
		MarketDataSource:     values.Source,
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
		Leverage:             10,
		MaximumOrderNotional: "100",
		StrategyInstanceId:   3,
	}
	cfg := &venueconfig.RuntimeConfig{Grid: &common.GridParameters{
		LowerPrice:       "90",
		UpperPrice:       "110",
		GridCount:        5,
		TotalBudget:      "500",
		PerOrderNotional: "100",
		InventoryLimit:   "2",
		MaxMarketAgeMs:   1000,
	}}
	book := &common.VenueBook{
		Symbol:       "BTCUSDT",
		Source:       values.Source,
		EventAtMs:    time.Now().UnixMilli(),
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
		Bids:         []*common.VenueLevel{{Price: "99.9"}},
		Asks:         []*common.VenueLevel{{Price: "100.1"}},
	}
	snapshot := &common.VenueSnapshot{
		Scope:        values.Scope(profile),
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
		Market: &common.VenueMarket{
			MinimumQuantity: "0.001",
			MinimumNotional: "1",
			MaximumQuantity: "100",
		},
		Positions: []*common.VenuePosition{{
			Symbol:         profile.Symbol,
			SignedQuantity: "1.5",
		}},
	}
	targets, err := instanceTargets(cfg, profile, book, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	buys, sells := decimal.Zero, decimal.Zero
	for _, target := range targets {
		quantity := decimal.RequireFromString(target.Quantity)
		if target.Side == string(domain.SideBuy) {
			buys = buys.Add(quantity)
			if target.Price != "90" && target.Price != "95" {
				t.Fatal("buy grid price moved")
			}
		} else {
			sells = sells.Add(quantity)
			if target.Price != "105" && target.Price != "110" {
				t.Fatal("sell grid price moved")
			}
		}
	}
	if !buys.IsPositive() || buys.GreaterThan(decimal.RequireFromString("0.5")) || sells.GreaterThan(decimal.RequireFromString("3.5")) {
		t.Fatal("grid exceeded directional inventory room")
	}
	book.Bids[0].Price, book.Asks[0].Price = "120", "121"
	if _, err := instanceTargets(cfg, profile, book, snapshot); !errors.Is(err, domain.ErrGridReferenceOutside) {
		t.Fatalf("outside grid range: %v", err)
	}
}
