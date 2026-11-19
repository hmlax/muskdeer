//go:build integration

package venue

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func fullSafeRefresh(cycle *quoteCycle) {
	cycle.bridgeSlots = 1
	for index, target := range cycle.targets {
		cycle.snapshot.Orders = append(cycle.snapshot.Orders, &common.VenueOrder{
			OrderId:          strconv.Itoa(index + 1),
			ClientId:         native.InstanceClientPrefix(cycle.profile) + strconv.Itoa(index),
			Side:             target.Side,
			Price:            target.Price,
			Quantity:         "0.05",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
	}
}

func TestOptionalRefreshCannotUseReservedRefillConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime, cycle, client, _ := batchQuoteSetup(ctx, t, 8)
	fullSafeRefresh(cycle)
	runtime.quoteRefreshSlots = make(chan struct{}, maximumOptionalQuoteSends)
	for range maximumOptionalQuoteSends {
		runtime.quoteRefreshSlots <- struct{}{}
	}
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	select {
	case err := <-done:
		if err != nil || client.calls.Load() != 0 || cycle.result.Placed != 0 || !cycle.retained.pending || len(runtime.quoteSlots) != 0 || len(runtime.quoteRefreshSlots) != maximumOptionalQuoteSends {
			t.Fatalf("optional refresh consumed refill capacity: err=%v calls=%d placed=%d pending=%t transport=%d optional=%d", err, client.calls.Load(), cycle.result.Placed, cycle.retained.pending, len(runtime.quoteSlots), len(runtime.quoteRefreshSlots))
		}
	case <-time.After(500 * time.Millisecond):
		cancel()
		joined := <-done
		t.Fatalf("optional refresh occupied refill reserve: calls=%d err=%v", client.calls.Load(), joined)
	}
}

func TestMissingDepthBypassesFullOptionalBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime, cycle, client, _ := batchQuoteSetup(ctx, t, 8)
	runtime.quoteRefreshSlots = make(chan struct{}, maximumOptionalQuoteSends)
	for range maximumOptionalQuoteSends {
		runtime.quoteRefreshSlots <- struct{}{}
	}
	close(client.release)
	if err := runtime.fillQuoteDepth(ctx, cycle); err != nil {
		t.Fatal(err)
	}
	if client.calls.Load() != 8 || cycle.result.Placed != 8 || len(runtime.quoteSlots) != 0 || len(runtime.quoteRefreshSlots) != maximumOptionalQuoteSends {
		t.Fatalf("refill waited on optional refresh: calls=%d placed=%d transport=%d optional=%d", client.calls.Load(), cycle.result.Placed, len(runtime.quoteSlots), len(runtime.quoteRefreshSlots))
	}
}

func TestMissingBestPriceBypassesFullOptionalBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime, cycle, client, _ := batchQuoteSetup(ctx, t, 8)
	fullSafeRefresh(cycle)
	cycle.snapshot.Orders[0].Price = "98.2"
	if cycle.needsQuoteDepth() {
		t.Fatal("test did not retain full non-crossing depth")
	}
	runtime.quoteRefreshSlots = make(chan struct{}, maximumOptionalQuoteSends)
	for range maximumOptionalQuoteSends {
		runtime.quoteRefreshSlots <- struct{}{}
	}
	close(client.release)
	if err := runtime.fillQuoteDepth(ctx, cycle); err != nil {
		t.Fatal(err)
	}
	if client.calls.Load() != 1 || cycle.result.Placed != 1 || len(runtime.quoteSlots) != 0 || len(runtime.quoteRefreshSlots) != maximumOptionalQuoteSends {
		t.Fatalf("new best price waited behind quantity refreshes: calls=%d placed=%d transport=%d optional=%d", client.calls.Load(), cycle.result.Placed, len(runtime.quoteSlots), len(runtime.quoteRefreshSlots))
	}
}

func TestCancelledOptionalSendsReturnBothBudgets(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runtime, cycle, client, _ := batchQuoteSetup(ctx, t, 8)
	fullSafeRefresh(cycle)
	runtime.quoteRefreshSlots = make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	select {
	case <-client.started:
	case <-ctx.Done():
		joined := <-done
		t.Fatalf("optional send did not start: %v", joined)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wave returned %v", err)
	}
	if client.calls.Load() != 1 || client.active.Load() != 0 || len(runtime.quoteSlots) != 0 || len(runtime.quoteRefreshSlots) != 0 {
		t.Fatalf("optional wave leaked or exceeded its budget: calls=%d active=%d transport=%d optional=%d", client.calls.Load(), client.active.Load(), len(runtime.quoteSlots), len(runtime.quoteRefreshSlots))
	}
}
