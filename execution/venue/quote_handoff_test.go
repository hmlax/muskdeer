package venue

import (
	"context"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/logging"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestNewPriceStopsRetirementAfterTheJoinedWave(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client := &heldCancelClient{
		started: make(chan uint64, 8),
		release: make(chan struct{}),
	}
	runtime := &Runtime{
		cfg: &venueconfig.RuntimeConfig{
			InstanceID: 1,
			Template:   domain.StrategyTemplateMarketMaker,
		},
		client:  &atomic.Pointer[gatewaySession]{},
		mailbox: newQuoteMailbox(),
	}
	runtime.client.Store(&gatewaySession{Client: client})
	previous := mailboxTestPlan(10)
	previous.Targets[0].Side = string(domain.SideBuy)
	cycle := &quoteCycle{
		profile:  &common.VenueProfilePolicy{MaximumOrderNotional: "100"},
		plan:     previous,
		retained: &quoteRetention{},
		snapshot: &common.VenueSnapshot{ValidUntilMs: time.Now().Add(2 * time.Second).UnixMilli()},
		result:   &pb.QuoteResult{},
	}
	var orders []*common.VenueOrder
	for id := 1; id <= 8; id++ {
		orders = append(orders, &common.VenueOrder{OrderId: strconv.Itoa(id)})
	}
	done := make(chan error, 1)
	go func() { done <- runtime.cancelQuoteRetirements(ctx, cycle, orders) }()
	for range maximumQuoteWave {
		select {
		case <-client.started:
		case <-ctx.Done():
			close(client.release)
			t.Fatalf("first retirement wave did not start: %v", <-done)
		}
	}
	next := proto.CloneOf(previous)
	next.Book.Sequence++
	next.Targets[0].Price = "97"
	runtime.mailbox.offer(next)
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if client.calls.Load() != maximumQuoteWave || client.active.Load() != 0 || cycle.result.Cancelled != maximumQuoteWave || !cycle.retained.pending {
		t.Fatalf("obsolete retirement continued or failed to join: calls=%d active=%d cancelled=%d pending=%t", client.calls.Load(), client.active.Load(), cycle.result.Cancelled, cycle.retained.pending)
	}
}

func TestNewAcceptedPriceInvalidatesObsoleteQuoteRetirement(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"fresh", "expired_intent", "quantity_only", "expired_account", "unsafe_new_price"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			logger, err := logging.New(t.Context(), nil, logging.Identity{
				Service:  "execution",
				Instance: "quote-handoff-test",
			}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := logger.Close(); err != nil {
					t.Error(err)
				}
			})
			profile := &common.VenueProfilePolicy{
				UserId:               1,
				FundId:               1,
				Symbol:               "BTCUSDT",
				StrategyInstanceId:   1,
				MaximumOrderNotional: "100",
			}
			previous := mailboxTestPlan(10)
			previous.Targets = []*common.VenueTarget{
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
			next := proto.CloneOf(previous)
			next.Book.Sequence++
			next.Targets[0].Price, next.Targets[1].Price = "97", "98"
			if kind == "expired_intent" {
				next.Book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			}
			if kind == "quantity_only" {
				next.Targets[0].Price, next.Targets[1].Price = "99", "101"
				next.Targets[0].Quantity = "0.2"
			}
			if kind == "unsafe_new_price" {
				next.Targets[0].Price, next.Targets[1].Price = "95", "96"
			}
			client := &heldCancelClient{
				started: make(chan uint64, 2),
				release: make(chan struct{}),
			}
			close(client.release)
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: 1,
					Template:   domain.StrategyTemplateMarketMaker,
				},
				logger:  logger,
				mailbox: newQuoteMailbox(),
				client:  &atomic.Pointer[gatewaySession]{},
			}
			runtime.client.Store(&gatewaySession{Client: client})
			runtime.mailbox.offer(next)
			snapshot := &common.VenueSnapshot{
				StartedAtMs:  time.Now().UnixMilli(),
				ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
				Orders: []*common.VenueOrder{
					{
						OrderId:          "1",
						ClientId:         native.InstanceClientPrefix(profile) + "bid",
						Side:             string(domain.SideBuy),
						Price:            "97",
						Quantity:         "0.1",
						ExecutedQuantity: "0",
						Status:           native.StatusOpen,
					},
					{
						OrderId:          "2",
						ClientId:         native.InstanceClientPrefix(profile) + "ask",
						Side:             string(domain.SideSell),
						Price:            "98",
						Quantity:         "0.1",
						ExecutedQuantity: "0",
						Status:           native.StatusOpen,
					},
				},
			}
			if kind == "expired_account" {
				snapshot.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
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
			cycle := &quoteCycle{
				profile:     profile,
				plan:        previous,
				bridgeSlots: 1,
				targets:     previous.Targets,
				snapshot:    snapshot,
				retained: &quoteRetention{
					orders:  make(map[string]bool),
					targets: make(map[string]bool),
				},
				result:    &pb.QuoteResult{State: native.StateRecovering},
				authority: LiveAuthority{Book: previous.Book},
			}
			_, err = runtime.finishQuoteDepth(t.Context(), cycle, wanted)
			if kind == "expired_account" || kind == "unsafe_new_price" {
				if err == nil || client.calls.Load() != 0 {
					t.Fatalf("unsafe handoff deferred protection: %v calls=%d", err, client.calls.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if kind == "fresh" {
				want = 0
			}
			if client.calls.Load() != want || !cycle.retained.pending {
				t.Fatalf("obsolete target retired the current best price: calls=%d want=%d pending=%t", client.calls.Load(), want, cycle.retained.pending)
			}
		})
	}
}
