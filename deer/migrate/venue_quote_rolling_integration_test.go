//go:build integration

package migrate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"

	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestVenueNativeSizeRefreshRetainsNinetyPercentWhileAwaitingProof(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{
		depth:  20,
		resize: true,
	})
}

type nativeRollingCheck struct {
	sdk    *nativeSDKFixture
	market *nativeMarketFixture
	depth  int
}

func assertNativeRollingResize(ctx context.Context, t *testing.T, check nativeRollingCheck) {
	t.Helper()
	release := sync.OnceFunc(func() { close(check.sdk.historyRelease) })
	t.Cleanup(release)
	check.sdk.historyHold.Store(true)
	check.market.quantity.Store(2)
	select {
	case <-check.sdk.historyEntered:
	case <-ctx.Done():
		t.Fatal("size refresh did not reach terminal-history barrier")
	}
	counts := make(map[pb.Side]int)
	check.sdk.mu.Lock()
	for _, order := range check.sdk.orders {
		if native.Active(order.Status) {
			counts[order.Side]++
		}
	}
	check.sdk.mu.Unlock()
	if counts[pb.Side_SIDE_BUY] < 18 || counts[pb.Side_SIDE_SELL] < 18 {
		t.Fatalf("size refresh awaiting terminal proof kept buys=%d sells=%d, want at least 18/18", counts[pb.Side_SIDE_BUY], counts[pb.Side_SIDE_SELL])
	}
	t.Logf("terminal-history barrier: buys=%d sells=%d", counts[pb.Side_SIDE_BUY], counts[pb.Side_SIDE_SELL])
	release()
	// Twenty levels require ten completed maintenance batches. Use this
	// scenario's existing 15s lifecycle, not the single-level helper's 5s.
	started := time.Now()
	for nativeResizedCount(check.sdk) != 2*check.depth {
		if err := native.Wait(ctx, 20*time.Millisecond); err != nil {
			t.Fatalf("twenty-level size refresh did not converge within the owned lifecycle: %v", err)
		}
	}
	t.Logf("twenty-level size refresh converged in %s", time.Since(started))
}

func nativeResizedCount(sdk *nativeSDKFixture) int {
	sdk.mu.Lock()
	defer sdk.mu.Unlock()
	current := 0
	for _, order := range sdk.orders {
		if native.Active(order.Status) && order.Quantity == 200 {
			current++
		}
	}
	return current
}

type nativeRepriceCheck struct {
	sdk    *nativeSDKFixture
	market *nativeMarketFixture
	offset int64
}

func assertNativeReprice(ctx context.Context, t *testing.T, check nativeRepriceCheck) {
	t.Helper()
	release := sync.OnceFunc(func() { close(check.sdk.historyRelease) })
	t.Cleanup(release)
	check.sdk.historyHold.Store(true)
	check.market.offset.Store(check.offset)
	select {
	case <-check.sdk.historyEntered:
	case <-ctx.Done():
		t.Fatal("repricing did not reach terminal-history barrier")
	}
	check.sdk.mu.Lock()
	buys, sells := 0, 0
	for _, order := range check.sdk.orders {
		if native.Active(order.Status) {
			if order.Side == pb.Side_SIDE_BUY {
				buys++
			} else {
				sells++
			}
		}
	}
	check.sdk.mu.Unlock()
	if buys == 0 || sells == 0 {
		t.Fatalf("repricing while cancel proof pending has buys=%d sells=%d, want both sides present", buys, sells)
	}
	release()
	nativeWait(ctx, t, func() bool {
		check.sdk.mu.Lock()
		defer check.sdk.mu.Unlock()
		current := 0
		bid := decimal.RequireFromString("99.9").Add(decimal.NewFromInt(check.offset).Shift(-1)).String()
		ask := decimal.RequireFromString("100.1").Add(decimal.NewFromInt(check.offset).Shift(-1)).String()
		for _, order := range check.sdk.orders {
			if native.Active(order.Status) && (order.Price == bid || order.Price == ask) {
				current++
			}
		}
		return current == 2
	})
}
