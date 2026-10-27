//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketfeed"
	"github.com/hmlax/muskdeer/internal/marketworker"
	"github.com/hmlax/muskdeer/internal/repository"
)

type monitoredMarketStream struct {
	stream marketworker.Stream
	active *atomic.Int32
	peak   *atomic.Int32
}

func (s *monitoredMarketStream) Run(ctx context.Context, observe marketfeed.Observer) error {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.peak.Load(); active > previous; previous = s.peak.Load() {
		if s.peak.CompareAndSwap(previous, active) {
			break
		}
	}
	return s.stream.Run(ctx, observe)
}

func TestOwnedMarketWorkersRenewStandbyAndTakeOverWithoutOverlappingStreams(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feed, fixtureAuthority, snapshots := marketSnapshotFixture(ctx, t, db)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Release(ctx, fixtureAuthority); err != nil {
		t.Fatal(err)
	}
	var active, peak atomic.Int32
	var owners [2]string
	var cancelNode [2]context.CancelFunc
	var results [2]chan error
	t.Cleanup(func() {
		for index := range 2 {
			if cancelNode[index] != nil {
				cancelNode[index]()
			}
			if results[index] != nil {
				if err := <-results[index]; err != nil && !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			}
		}
	})
	for index := range 2 {
		owners[index], err = ownerid.New(ctx, "market-worker")
		if err != nil {
			t.Fatal(err)
		}
		worker, err := marketworker.New(marketworker.Config{
			Feed:             feed,
			OwnerID:          owners[index],
			LeaseTTL:         3 * time.Second,
			OperationTimeout: 200 * time.Millisecond,
			RetryInterval:    20 * time.Millisecond,
		}, marketworker.Dependencies{
			Leases:    leases,
			Snapshots: snapshots,
			Factory: func(binding domain.MarketFeedDefinition) (marketworker.Stream, error) {
				stream, err := marketworker.NewReplay(binding, marketworker.ReplayConfig{
					Bid:             decimal.RequireFromString("99.99"),
					Ask:             decimal.RequireFromString("100.01"),
					Quantity:        decimal.NewFromInt(1),
					Interval:        50 * time.Millisecond,
					CallbackTimeout: 200 * time.Millisecond,
				})
				if err != nil {
					return nil, err
				}
				return &monitoredMarketStream{
					stream: stream,
					active: &active,
					peak:   &peak,
				}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		run, stop := context.WithCancel(ctx)
		cancelNode[index] = stop
		results[index] = make(chan error, 1)
		result := results[index]
		go func() { result <- worker.Run(run) }()
	}
	first := waitMarketPublication(ctx, t, snapshots, feed.ID, func(value domain.MarketPublication) bool {
		return value.Reading.Quality == domain.MarketQualityHealthy
	})
	ownerIndex := 0
	if first.OwnerID == owners[1] {
		ownerIndex = 1
	}
	if first.OwnerID != owners[ownerIndex] || first.Reading.Mid.String() != "100" || active.Load() != 1 {
		t.Fatalf("source owner/reference mismatch: %+v, active=%d", first, active.Load())
	}
	// Remain alive beyond a renewal tick and prove a current publication advances.
	stable := waitMarketPublication(ctx, t, snapshots, feed.ID, func(value domain.MarketPublication) bool {
		return value.OwnerEpoch == first.OwnerEpoch && value.Revision > first.Revision+20 && value.Reading.Quality == domain.MarketQualityHealthy
	})
	cancelNode[ownerIndex]()
	if err := <-results[ownerIndex]; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	results[ownerIndex] = nil
	next := waitMarketPublication(ctx, t, snapshots, feed.ID, func(value domain.MarketPublication) bool {
		return value.OwnerEpoch > first.OwnerEpoch && value.Reading.Quality == domain.MarketQualityHealthy
	})
	if next.OwnerEpoch != first.OwnerEpoch+1 || next.OwnerID == first.OwnerID || !next.Baseline.Mid.Equal(stable.Baseline.Mid) || peak.Load() != 1 {
		t.Fatalf("takeover overlapped stream or lost baseline: next=%+v, peak=%d", next, peak.Load())
	}
}

func waitMarketPublication(ctx context.Context, t *testing.T, store domain.MarketRepository, feedID int64, matches func(domain.MarketPublication) bool) domain.MarketPublication {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		value, err := store.Get(call, feedID)
		cancel()
		if err == nil && matches(value) {
			return value
		}
		if err != nil && !errors.Is(err, domain.ErrMarketMissing) {
			t.Fatalf("current market observation failed: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("market publication did not reach the expected state")
		case <-ticker.C:
		}
	}
}
