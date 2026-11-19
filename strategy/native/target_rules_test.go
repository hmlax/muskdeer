package native

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func targetRuleFixture() (*venueconfig.RuntimeConfig, *common.VenueProfilePolicy, *common.VenueBook, *common.VenueSnapshot) {
	cfg := &venueconfig.RuntimeConfig{
		InstanceID: 1,
		Template:   domain.StrategyTemplateMarketMaker,
		Venue: &common.VenuePolicy{
			Depth:            20,
			QuantityFraction: "0.1",
		},
	}
	profile := &common.VenueProfilePolicy{
		UserId:               1,
		FundId:               1,
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "1000",
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
		Leverage:             10,
	}
	book := &common.VenueBook{
		Symbol:       profile.Symbol,
		Source:       values.Source,
		Segment:      values.Segment,
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
		Bids: []*common.VenueLevel{{
			Price:    "100.1",
			Quantity: "0.1",
		}},
		Asks: []*common.VenueLevel{{
			Price:    "100.2",
			Quantity: "0.1",
		}},
	}
	snapshot := &common.VenueSnapshot{
		Scope:        values.Scope(profile),
		StartedAtMs:  time.Now().UnixMilli(),
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
		Market: &common.VenueMarket{
			PriceTick:       profile.PriceTick,
			QuantityStep:    profile.QuantityStep,
			MinimumQuantity: "0.001",
			MaximumQuantity: "0",
			MinimumNotional: "0.1",
			MaximumLeverage: 10,
			MarkPrice:       "100.1",
			BestBid:         "100.1",
			BestAsk:         "100.2",
		},
	}
	return cfg, profile, book, snapshot
}

func TestMakerTargetRulesKeepNewPricesFlowingWhenPortfolioIsUnavailable(t *testing.T) {
	t.Parallel()
	cfg, profile, book, snapshot := targetRuleFixture()
	rules := &targetRules{}
	if _, err := rules.targets(t.Context(), cfg, profile, book, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	// A settled account read can be unavailable while the public source moves.
	// Only the immutable market rules may be reused, never the old funds/TTL.
	snapshot.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
	snapshot.Market.MinimumQuantity = "100"
	book.Bids[0].Price, book.Asks[0].Price = "110.1", "110.2"
	targets, err := rules.targets(t.Context(), cfg, profile, book, nil, status.Error(codes.Unavailable, "portfolio expired"))
	if err != nil || len(targets) != 2 || targets[0].Price != "110.1" || targets[1].Price != "110.2" || targets[0].Quantity != "0.01" {
		t.Fatalf("new source target stalled behind account state: targets=%v err=%v", targets, err)
	}
	if rules.market.MarkPrice != "" || rules.market.BestBid != "" || rules.market.BestAsk != "" || rules.market.WorstFeeRate != "" {
		t.Fatal("dynamic market observations entered the rules cache")
	}
}

func TestMakerTargetRulesDoNotBypassAdmissionOrInventoryInputs(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"cold", "source_expired", "profile_changed", "snapshot_expired", "wrong_scope", "permission", "cancelled", "grid", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg, profile, book, snapshot := targetRuleFixture()
			rules := &targetRules{}
			if kind != "cold" {
				if _, err := rules.targets(t.Context(), cfg, profile, book, snapshot, nil); err != nil {
					t.Fatal(err)
				}
			}
			ctx := t.Context()
			readErr := status.Error(codes.Unavailable, "portfolio expired")
			var fresh *common.VenueSnapshot
			switch kind {
			case "source_expired":
				book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "profile_changed":
				profile = proto.CloneOf(profile)
				profile.FundId++
			case "snapshot_expired":
				fresh, readErr = snapshot, nil
				fresh.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "wrong_scope":
				fresh, readErr = snapshot, nil
				fresh.Scope.FundId++
			case "permission":
				readErr = status.Error(codes.PermissionDenied, "denied")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "grid":
				cfg.Template = domain.StrategyTemplateGrid
				cfg.Grid = &common.GridParameters{}
			case "legacy":
				cfg.InstanceID = 0
			}
			if targets, err := rules.targets(ctx, cfg, profile, book, fresh, readErr); err == nil || len(targets) != 0 {
				t.Fatalf("invalid input produced targets: %v %v", targets, err)
			}
		})
	}
}
