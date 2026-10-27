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
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestMarketBindingsPersistPerSourceAndRejectIdentityOrPolicyChanges(t *testing.T) {
	db, cfg := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	instrument := marketInstrumentFixture(ctx, t, db)
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	input := domain.MarketFeedDefinition{
		Source:        domain.MarketSourceBinance,
		InstrumentID:  instrument,
		Symbol:        "BTCUSDT",
		Mode:          domain.MarketModeLive,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        marketquality.DefaultPolicy(),
	}
	first, err := feeds.Create(ctx, input)
	if err != nil || first.ID <= 0 {
		t.Fatalf("create binding: %+v, %v", first, err)
	}
	if again, err := feeds.Create(ctx, input); err != nil || again.ID != first.ID {
		t.Fatalf("idempotent creation: %+v, %v", again, err)
	}
	changed := input
	changed.Symbol = "ETHUSDT"
	if _, err := feeds.Create(ctx, changed); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatalf("existing source rebound to another product: %v", err)
	}
	changed = input
	changed.Policy.MaximumAge = time.Second
	if _, err := feeds.Create(ctx, changed); !errors.Is(err, domain.ErrMarketFeedConflict) {
		t.Fatalf("quality policy changed without an explicit version flow: %v", err)
	}
	other := input
	other.Source, other.Symbol = domain.MarketSourceOKX, "BTC-USDT"
	second, err := feeds.Create(ctx, other)
	if err != nil || second.ID == first.ID {
		t.Fatalf("sources were not isolated: %+v, %v", second, err)
	}
	replay := input
	replay.Mode = domain.MarketModeReplay
	third, err := feeds.Create(ctx, replay)
	if err != nil || third.ID == first.ID {
		t.Fatalf("replay replaced live stream: %+v, %v", third, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ?", first.ID).Update("external_symbol", "ETHUSDT").Error; err == nil {
		t.Fatal("database allowed immutable binding rewrite")
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ?", first.ID).Delete(&modelv1.MarketFeed{}).Error; err == nil {
		t.Fatal("database allowed binding deletion")
	}
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ?", instrument).Update("base_asset", "ETH").Error; err == nil {
		t.Fatal("bound product silently changed underlying assets")
	}
	client, err := postgres.New(ctx, cfg, "market-binding-restart")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	restarted, err := repository.NewMarketFeeds(client.GORM())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := restarted.Find(ctx, domain.MarketSourceBinance, instrument, domain.MarketModeLive)
	if err != nil || saved.ID != first.ID || saved.Symbol != input.Symbol || !saved.Policy.MaximumSpreadBP.Equal(input.Policy.MaximumSpreadBP) || saved.Policy.MaximumAge != input.Policy.MaximumAge {
		t.Fatalf("restart lost exact source/policy: %+v, %v", saved, err)
	}
}

func marketInstrumentFixture(ctx context.Context, t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	return instrument.Id
}

func TestMarketPublisherLeaseHasOneWinnerAndRetainsFencingEpoch(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := feeds.Create(ctx, domain.MarketFeedDefinition{
		Source:        domain.MarketSourceOKX,
		InstrumentID:  marketInstrumentFixture(ctx, t, db),
		Symbol:        "BTC-USDT",
		Mode:          domain.MarketModeReplay,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        marketquality.DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feed.ID,
	}
	var workers sync.WaitGroup
	results := make(chan domain.LeaseToken, 4)
	failures := make(chan error, 4)
	for range 4 {
		workers.Go(func() {
			owner, err := ownerid.New(ctx, "market-node")
			if err != nil {
				failures <- err
				return
			}
			token, err := leases.Acquire(ctx, key, owner, 2*time.Second)
			if err != nil {
				failures <- err
				return
			}
			results <- token
		})
	}
	workers.Wait()
	if len(results) != 1 || len(failures) != 3 {
		t.Fatalf("one source had multiple publishers: winners=%d, failures=%d", len(results), len(failures))
	}
	for range 3 {
		if err := <-failures; !errors.Is(err, domain.ErrLeaseHeld) {
			t.Fatalf("unexpected acquisition failure: %v", err)
		}
	}
	first := <-results
	if err := leases.Release(ctx, first); err != nil {
		t.Fatal(err)
	}
	nextOwner, err := ownerid.New(ctx, "market-next")
	if err != nil {
		t.Fatal(err)
	}
	second, err := leases.Acquire(ctx, key, nextOwner, time.Second)
	if err != nil || second.Epoch != first.Epoch+1 {
		t.Fatalf("source takeover lost epoch: %+v, %v", second, err)
	}
	if _, err := leases.Renew(ctx, first, time.Second); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("old publisher revived: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).Update("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	second.ExpiresAt = time.Now().Add(time.Hour)
	if _, err := leases.Renew(ctx, second, time.Second); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("client time extended expired authority: %v", err)
	}
	third, err := leases.Acquire(ctx, key, second.OwnerID, time.Second)
	if err != nil || third.Epoch != second.Epoch+1 {
		t.Fatalf("expired same incarnation reused epoch: %+v, %v", third, err)
	}
}
