package himalaya

import (
	"testing"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestQuoteRefreshKeepsBothSidesUntilTheirReplacementsExist(t *testing.T) {
	t.Parallel()
	profile := &configv1.HimalayaProfile{
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "500",
	}
	oldBuy := retentionOrder("1", domain.SideBuy, "100")
	oldSell := retentionOrder("2", domain.SideSell, "100.4")
	wanted := map[string]*SubmitRequest{
		"BUY/100.1": {
			Side:     domain.SideBuy,
			Price:    decimal.RequireFromString("100.1"),
			Quantity: decimal.NewFromInt(1),
		},
		"SELL/100.5": {
			Side:     domain.SideSell,
			Price:    decimal.RequireFromString("100.5"),
			Quantity: decimal.NewFromInt(1),
		},
	}
	for _, replaced := range []bool{false, true} {
		snapshot := &commonv1.HimalayaSnapshot{Orders: []*commonv1.HimalayaOrder{oldBuy, oldSell}}
		if replaced {
			snapshot.Orders = append(snapshot.Orders, retentionOrder("3", domain.SideBuy, "100.1"), retentionOrder("4", domain.SideSell, "100.5"))
		}
		kept, err := retainQuoteOrders(profile, snapshot, wanted, true)
		if err != nil {
			t.Fatal(err)
		}
		if replaced {
			if kept.orders["1"] || kept.orders["2"] || !kept.orders["3"] || !kept.orders["4"] || len(kept.targets) != 2 {
				t.Fatalf("retainQuoteOrders(replacements exist) = %v, want new bid/ask only", kept)
			}
		} else if !kept.orders["1"] || !kept.orders["2"] || len(kept.targets) != 0 {
			t.Fatalf("retainQuoteOrders(before replacements) = %v, want both old sides retained without satisfying new targets", kept)
		}
	}
}

func retentionOrder(id string, side domain.Side, price string) *commonv1.HimalayaOrder {
	return &commonv1.HimalayaOrder{
		OrderId:          id,
		ClientId:         native.ClientPrefix + "BTCUSDT_" + id,
		Side:             string(side),
		Price:            price,
		Quantity:         "1",
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	}
}

func TestQuoteSubmissionStartsBothSidesBeforeFillingTheLadder(t *testing.T) {
	t.Parallel()
	targets := []*commonv1.HimalayaTarget{
		{Side: string(domain.SideBuy), Price: "100"},
		{Side: string(domain.SideBuy), Price: "99.9"},
		{Side: string(domain.SideBuy), Price: "99.8"},
		{Side: string(domain.SideSell), Price: "100.1"},
		{Side: string(domain.SideSell), Price: "100.2"},
	}
	ordered := quoteSubmissionOrder(targets)
	if len(ordered) != 5 || ordered[0] != targets[0] || ordered[1] != targets[3] || ordered[2] != targets[1] || ordered[3] != targets[4] || ordered[4] != targets[2] {
		t.Fatalf("quoteSubmissionOrder = %v, want alternating bid/ask then remaining bid", ordered)
	}
	if targets[1].Side != string(domain.SideBuy) || targets[3].Side != string(domain.SideSell) {
		t.Fatal("quoteSubmissionOrder mutated the caller's plan")
	}
}

func TestQuoteRefreshDoesNotBridgeCrossingLegacyOrOversizedOrders(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"crossing", "legacy", "over_cap"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			profile := &configv1.HimalayaProfile{
				Symbol:               "BTCUSDT",
				MaximumOrderNotional: "500",
			}
			buy := retentionOrder("1", domain.SideBuy, "100")
			sell := retentionOrder("2", domain.SideSell, "100.4")
			switch kind {
			case "crossing":
				buy.Price, sell.Price = "100.6", "100"
			case "legacy":
				buy.ClientId = native.LegacyPrefix + "BTCUSDT_1"
				sell.ClientId = native.LegacyPrefix + "BTCUSDT_2"
			case "over_cap":
				profile.MaximumOrderNotional = "50"
			}
			wanted := map[string]*SubmitRequest{
				"BUY/100.1": {
					Side:     domain.SideBuy,
					Price:    decimal.RequireFromString("100.1"),
					Quantity: decimal.NewFromInt(1),
				},
				"SELL/100.5": {
					Side:     domain.SideSell,
					Price:    decimal.RequireFromString("100.5"),
					Quantity: decimal.NewFromInt(1),
				},
			}
			kept, err := retainQuoteOrders(profile, &commonv1.HimalayaSnapshot{Orders: []*commonv1.HimalayaOrder{buy, sell}}, wanted, false)
			if err != nil || len(kept.orders) != 0 {
				t.Fatalf("retainQuoteOrders(%s) = %v, %v, want no unsafe bridges", kind, kept, err)
			}
		})
	}
}
