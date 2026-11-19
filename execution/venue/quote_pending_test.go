package venue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func pendingQuoteFixture() (*quoteCycle, map[string]*SubmitRequest) {
	profile := &common.VenueProfilePolicy{
		Symbol:               "BTCUSDT",
		StrategyInstanceId:   1,
		MaximumOrderNotional: "100",
	}
	snapshot := &common.VenueSnapshot{
		Market: &common.VenueMarket{MarkPrice: "100"},
	}
	for index, price := range []string{"99", "98", "101"} {
		side := domain.SideBuy
		if index == 2 {
			side = domain.SideSell
		}
		id := []string{"1", "2", "3"}[index]
		snapshot.Orders = append(snapshot.Orders, &common.VenueOrder{
			OrderId:          id,
			ClientId:         native.InstanceClientPrefix(profile) + id,
			Side:             string(side),
			Price:            price,
			Quantity:         "0.1",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
	}
	targets := []*common.VenueTarget{
		{
			Side:     string(domain.SideBuy),
			Price:    "99",
			Quantity: "0.1",
		},
		{
			Side:     string(domain.SideSell),
			Price:    "101",
			Quantity: "0.1",
		},
	}
	wanted := map[string]*SubmitRequest{
		"BUY/99": {
			Side:     domain.SideBuy,
			Price:    decimal.NewFromInt(99),
			Quantity: decimal.RequireFromString("0.1"),
		},
		"SELL/101": {
			Side:     domain.SideSell,
			Price:    decimal.NewFromInt(101),
			Quantity: decimal.RequireFromString("0.1"),
		},
	}
	return &quoteCycle{
		profile:     profile,
		bridgeSlots: 1,
		targets:     targets,
		snapshot:    snapshot,
		cancelling:  map[string]bool{"1": true},
		retained: &quoteRetention{
			orders: map[string]bool{
				"1": true,
				"3": true,
			},
			targets: make(map[string]bool),
		},
	}, wanted
}

func TestPriorUnconfirmedCancelCannotBackTheNextDepthFloor(t *testing.T) {
	t.Parallel()
	cycle, wanted := pendingQuoteFixture()
	selected := depthRetirements(cycle, wanted, decimal.RequireFromString("0.1"))
	// The earlier SDK owner can finish after its caller timed out. Apply every
	// outstanding cancel, including the preceding one, before checking depth.
	for _, order := range selected {
		order.Status = native.StatusCanceled
	}
	cycle.snapshot.Orders[0].Status = native.StatusCanceled
	buys := 0
	for _, order := range cycle.snapshot.Orders {
		if native.Active(order.Status) && order.Side == string(domain.SideBuy) {
			buys++
		}
	}
	if buys == 0 {
		t.Fatalf("late prior cancellation plus %d newly selected cancellations emptied the side", len(selected))
	}
}

func TestPriorCancelStillConsumesCapacityAndRisk(t *testing.T) {
	t.Parallel()
	cycle, _ := pendingQuoteFixture()
	if cycle.canPlace(cycle.targets[0]) {
		t.Fatal("pending cancellation falsely freed a venue slot")
	}
	gross, err := snapshotGross(cycle.snapshot, &common.VenueBook{})
	if err != nil || !gross.Equal(decimal.RequireFromString("30.1")) {
		t.Fatalf("pending cancellation lost its notional reserve: %s %v", gross, err)
	}
}

func TestRoomMakingPreservesTheLastOrderWithoutAPendingCancel(t *testing.T) {
	t.Parallel()
	cycle, wanted := pendingQuoteFixture()
	cycle.snapshot.Orders[0], cycle.snapshot.Orders[1] = cycle.snapshot.Orders[1], cycle.snapshot.Orders[0]
	wanted["SELL/101"].Price = decimal.NewFromInt(97)
	orders, err := quoteRoomRetirements(cycle, wanted)
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range orders {
		if order.OrderId == "2" {
			t.Fatal("room making cancelled the last stable order while a prior cancel could still finish")
		}
	}
}

func TestFreshStillActivePendingCancelDoesNotFreezeFullCapacity(t *testing.T) {
	t.Parallel()
	cycle, wanted := pendingQuoteFixture()
	orders, err := quoteRoomRetirements(cycle, wanted)
	if err != nil || len(orders) != 1 || orders[0].OrderId != "1" {
		t.Fatalf("still visible pending cancellation blocked capacity: %v %v", orders, err)
	}
}

func TestCancellationOutcomeCannotMasqueradeAsStableLiquidity(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"acknowledged", "unknown", "not_dispatched", "cancelled_before_dispatch"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			client := &heldCancelClient{
				started: make(chan uint64, 1),
				release: make(chan struct{}),
			}
			close(client.release)
			switch kind {
			case "unknown":
				client.failure = errors.Join(domain.ErrOutcomeUnknown, context.DeadlineExceeded)
			case "not_dispatched":
				client.failure = gateway.ErrNotDispatched
			}
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: 1,
					Template:   domain.StrategyTemplateMarketMaker,
				},
				client: &atomic.Pointer[gatewaySession]{},
			}
			runtime.client.Store(&gatewaySession{Client: client})
			cycle, wanted := pendingQuoteFixture()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancelled_before_dispatch" {
				cancel()
			}
			err := runtime.cancelOne(ctx, domain.LeaseToken{}, cycle.profile, cycle.snapshot.Orders[0])
			if kind == "acknowledged" && err != nil || kind == "unknown" && !errors.Is(err, domain.ErrOutcomeUnknown) ||
				kind == "not_dispatched" && !errors.Is(err, gateway.ErrNotDispatched) || kind == "cancelled_before_dispatch" && !errors.Is(err, context.Canceled) {
				t.Fatalf("outcome identity changed: %v", err)
			}
			cycle.snapshot.StartedAtMs = time.Now().Add(time.Millisecond).UnixMilli()
			cycle.snapshot.ValidUntilMs = time.Now().Add(time.Second).UnixMilli()
			cycle.cancelling = runtime.pendingQuoteCancellations(cycle.snapshot)
			pending := kind == "acknowledged" || kind == "unknown"
			if cycle.cancelling["1"] != pending {
				t.Fatalf("cancel outcome=%s pending=%v", kind, cycle.cancelling)
			}
			retained, e := retainQuoteOrders(cycle.profile, quoteRetentionSnapshot(cycle.snapshot, cycle.cancelling), wanted, true, decimal.RequireFromString("0.1"))
			if e != nil || retained.targets["BUY/99"] == pending || cycle.needsQuoteTouch(gateway.QuoteRequest{
				Side:  domain.SideBuy,
				Price: decimal.NewFromInt(99),
			}) != pending {
				t.Fatalf("pending cancellation counted as a retained touch: %v %v", retained, e)
			}
			if cycle.canPlace(cycle.targets[0]) || len(cycle.snapshot.Orders) != 3 {
				t.Fatal("liquidity projection changed actual capacity or orders")
			}
			wantCalls := int64(1)
			if kind == "cancelled_before_dispatch" {
				wantCalls = 0
			}
			if client.calls.Load() != wantCalls {
				t.Fatal("cancellation dispatched again")
			}
		})
	}
}

func TestOnlyANewerFreshSDKAbsenceClearsCancellationHint(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"older", "same_millisecond", "expired", "fresh"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			observed := time.Now().Add(-time.Second).UnixMilli()
			runtime := &Runtime{}
			runtime.quoteCancellations.Store("1", &quoteCancellationMark{observedAtMs: observed})
			snapshot := &common.VenueSnapshot{
				StartedAtMs:  observed + 1,
				ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
			}
			switch kind {
			case "older":
				snapshot.StartedAtMs = observed - 1
			case "same_millisecond":
				snapshot.StartedAtMs = observed
			case "expired":
				snapshot.ValidUntilMs = observed + 2
			}
			pending := runtime.pendingQuoteCancellations(snapshot)
			_, stored := runtime.quoteCancellations.Load("1")
			want := kind != "fresh"
			if pending["1"] != want || stored != want {
				t.Fatalf("%s snapshot released hint: pending=%v stored=%t", kind, pending, stored)
			}
		})
	}
}
