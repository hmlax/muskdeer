//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestLeaseEpochsSurviveReleaseExpiryAndNewConnections(t *testing.T) {
	db, cfg := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	strategyID := leaseStrategyFixture(ctx, t, db)
	for _, key := range []domain.LeaseKey{
		{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: strategyID,
		},
		{
			Kind:      domain.LeaseKindExecution,
			AccountID: "fixture-shared-account",
		},
	} {
		store, err := repository.NewLeases(db)
		if err != nil {
			t.Fatal(err)
		}
		ownerA, err := ownerid.New(ctx, "node-a")
		if err != nil {
			t.Fatal(err)
		}
		ownerB, err := ownerid.New(ctx, "node-b")
		if err != nil {
			t.Fatal(err)
		}
		first, err := store.Acquire(ctx, key, ownerA, 2*time.Second)
		if err != nil || first.Epoch != 1 {
			t.Fatalf("first lease = %v, %v", first, err)
		}
		again, err := store.Acquire(ctx, key, ownerA, 2*time.Second)
		if err != nil || again.Epoch != first.Epoch || !again.ExpiresAt.Equal(first.ExpiresAt) {
			t.Fatalf("same-incarnation retry changed lease: %v, %v", again, err)
		}
		if _, err := store.Acquire(ctx, key, ownerB, 2*time.Second); !errors.Is(err, domain.ErrLeaseHeld) {
			t.Fatalf("competing owner acquired current lease: %v", err)
		}
		renewed, err := store.Renew(ctx, first, 3*time.Second)
		if err != nil || renewed.Epoch != first.Epoch || renewed.ExpiresAt.Before(first.ExpiresAt) {
			t.Fatalf("renewal = %v, %v", renewed, err)
		}
		if err := store.Release(ctx, renewed); err != nil {
			t.Fatal(err)
		}
		second, err := store.Acquire(ctx, key, ownerB, 2*time.Second)
		if err != nil || second.Epoch != first.Epoch+1 {
			t.Fatalf("takeover did not advance epoch: %v, %v", second, err)
		}
		if _, err := store.Renew(ctx, first, 2*time.Second); !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatalf("stale owner renewed successor lease: %v", err)
		}
		if err := store.Release(ctx, first); !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatalf("stale owner released successor lease: %v", err)
		}
		// Force persisted expiry as a controlled database-clock failure schedule.
		expireLeaseFixture(ctx, t, db, key)
		second.ExpiresAt = time.Now().Add(24 * time.Hour) // Untrusted client time cannot renew an expired record.
		if _, err := store.Renew(ctx, second, 2*time.Second); !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatalf("expired epoch was renewed: %v", err)
		}
		fresh := freshLeaseStore(ctx, t, cfg)
		third, err := fresh.Acquire(ctx, key, ownerB, 2*time.Second)
		if err != nil || third.Epoch != second.Epoch+1 {
			t.Fatalf("expired same owner reused an epoch: %v, %v", third, err)
		}
	}
}

func freshLeaseStore(ctx context.Context, t *testing.T, cfg *configv1.Postgres) domain.LeaseRepository {
	t.Helper()
	client, err := postgres.New(ctx, cfg, "lease-restarted-client")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	store, err := repository.NewLeases(client.GORM())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestConcurrentLeaseAcquisitionHasOneWinner(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "contended-account",
	}
	start := make(chan struct{})
	results := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			owner, err := ownerid.New(ctx, "contender")
			if err == nil {
				_, err = store.Acquire(ctx, key, owner, 5*time.Second)
			}
			results <- err
		})
	}
	close(start)
	workers.Wait()
	var winners int
	for range 4 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, domain.ErrLeaseHeld) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("lease contenders produced %d winners", winners)
	}
}

func leaseStrategyFixture(ctx context.Context, t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	var strategy *modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		var err error
		strategy, err = insertIdentity(ctx, tx, instrument.Id, "lease-strategy", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return strategy.Id
}

func expireLeaseFixture(ctx context.Context, t *testing.T, db *gorm.DB, key domain.LeaseKey) {
	t.Helper()
	var result *gorm.DB
	switch key.Kind {
	case domain.LeaseKindStrategy:
		result = db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Where("strategy_instance_id = ?", key.StrategyID).UpdateColumn("expires_at", 0)
	case domain.LeaseKindExecution:
		result = db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", key.AccountID).UpdateColumn("expires_at", 0)
	case domain.LeaseKindMarket:
		result = db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", key.MarketFeedID).UpdateColumn("expires_at", 0)
	default:
		t.Fatal("unsupported fixture lease kind")
		return
	}
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("lease expiry did not affect exactly one owner: %d, %v", result.RowsAffected, result.Error)
	}
}
