//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminMarketsReadExactSourceModeAndMissingSnapshotWithoutEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	store := adminMarketsStore(t, db)
	filter := adminMarketFilter()
	empty, err := store.List(ctx, actor.Token, filter)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty market list invented a healthy feed", err)
	}
	feed, authority, snapshots := marketSnapshotFixture(ctx, t, db)
	feeds := catalogFeedFixtures(ctx, t, db, feed.InstrumentID)
	missing, err := store.Detail(ctx, actor.Token, feeds[1].ID)
	if err != nil || missing.Snapshot != nil || missing.Feed.Source != domain.MarketSourceOKX || missing.Instrument.Rules.ID != feed.InstrumentID || missing.EvaluatedAt.UnixMilli() <= 0 {
		t.Fatal("unpublished OKX feed borrowed Binance data or lost metadata", err)
	}
	reading := marketReadingFixture(feed, 9007199254740993, "100")
	reading.Bid = decimal.RequireFromString("99.999999999999999999")
	reading.Ask = decimal.NewFromInt(100)
	reading.Mid = decimal.RequireFromString("99.9999999999999999995")
	if _, err := snapshots.Publish(ctx, authority, 0, reading); err != nil {
		t.Fatal(err)
	}
	before := adminMarketStoredSnapshot(ctx, t, db, feed.ID)
	assertAdminMarketPages(ctx, t, store, actor.Token, feed, feeds[1], reading)
	if !proto.Equal(before, adminMarketStoredSnapshot(ctx, t, db, feed.ID)) {
		t.Fatal("management read republished or changed a market snapshot")
	}
	filter.Mode = domain.MarketModeLive
	live, err := store.List(ctx, actor.Token, filter)
	if err != nil || live.Items == nil || len(live.Items) != 0 {
		t.Fatal("REPLAY data appeared as LIVE", err)
	}
	if _, err := store.Detail(ctx, actor.Token, feeds[1].ID+1); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing feed returned a substitute", err)
	}
	filter.InstrumentID = feed.InstrumentID + 1
	if _, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing product became an empty market choice", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketSnapshot{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
}

func assertAdminMarketPages(ctx context.Context, t *testing.T, store domain.AdminMarkets, token string, feed, other domain.MarketFeedDefinition, reading domain.MarketReading) {
	t.Helper()
	filter := adminMarketFilter()
	first, err := store.List(ctx, token, filter)
	if err != nil || len(first.Items) != 1 || first.NextAfterID != feed.ID || first.Items[0].Feed.ID != feed.ID {
		t.Fatal("market feed identity pagination failed", err)
	}
	value := first.Items[0]
	if value.Snapshot == nil || !value.Snapshot.PublisherCurrent || value.Snapshot.PublisherEpoch <= 0 || value.Snapshot.Reading.Sequence != reading.Sequence || value.Snapshot.Reading.Mid.String() != reading.Mid.String() || value.Snapshot.Reading.Bid.String() != reading.Bid.String() || value.Snapshot.Reading.Quality != domain.MarketQualityHealthy {
		t.Fatal("market read lost publisher, sequence or exact decimal precision")
	}
	filter.Page.AfterID = first.NextAfterID
	second, err := store.List(ctx, token, filter)
	if err != nil || len(second.Items) != 1 || second.NextAfterID != 0 || second.Items[0].Feed.ID != other.ID || second.Items[0].Snapshot != nil {
		t.Fatal("final market page borrowed another feed observation", err)
	}
	filter.Page.AfterID, filter.Source = 0, domain.MarketSourceOKX
	only, err := store.List(ctx, token, filter)
	if err != nil || len(only.Items) != 1 || only.Items[0].Feed.ID != other.ID {
		t.Fatal("exact market source filter failed", err)
	}
	value.Snapshot.Reading.Source = domain.MarketSourceOKX
	again, err := store.Detail(ctx, token, feed.ID)
	if err != nil || again.Snapshot == nil || again.Snapshot.Reading.Source != domain.MarketSourceBinance {
		t.Fatal("caller mutation changed a subsequent observation", err)
	}
}

func TestAdminMarketsRecheckDatabaseAgeAndCurrentPublisher(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	feed, authority, snapshots := marketSnapshotFixture(ctx, t, db)
	first, err := snapshots.Publish(ctx, authority, 0, marketReadingFixture(feed, 10, "100"))
	if err != nil {
		t.Fatal(err)
	}
	store := adminMarketsStore(t, db)
	waitMarketDatabaseExpiry(ctx, t, db, feed.ID, first.Reading.ValidUntil)
	stale, err := store.Detail(ctx, actor.Token, feed.ID)
	if err != nil || stale.Snapshot == nil || !stale.Snapshot.PublisherCurrent || stale.Snapshot.PublishedQuality != domain.MarketQualityHealthy || stale.Snapshot.Reading.Quality != domain.MarketQualityStale || stale.Snapshot.Reading.Reason != domain.MarketReasonAge || !stale.Snapshot.Reading.Mid.IsZero() || !stale.Snapshot.Reading.ValidUntil.IsZero() || stale.Snapshot.PublishedValidUntil != first.Reading.ValidUntil {
		t.Fatal("stored healthy reading outlived database freshness", err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Release(ctx, authority); err != nil {
		t.Fatal(err)
	}
	nextOwner, err := ownerid.New(ctx, "admin-market-takeover")
	if err != nil {
		t.Fatal(err)
	}
	next, err := leases.Acquire(ctx, authority.Key, nextOwner, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	assertAdminMarketUnavailable(ctx, t, store, actor.Token, feed.ID)
	if _, err := snapshots.Publish(ctx, next, first.Revision, marketReadingFixture(feed, 1, "100")); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Detail(ctx, actor.Token, feed.ID)
	if err != nil || recovered.Snapshot == nil || !recovered.Snapshot.PublisherCurrent || recovered.Snapshot.PublisherEpoch != next.Epoch || recovered.Snapshot.Reading.Quality != domain.MarketQualityHealthy {
		t.Fatal("new current publisher could not recover its own observation", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).Update("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	assertAdminMarketUnavailable(ctx, t, store, actor.Token, feed.ID)
}

func assertAdminMarketUnavailable(ctx context.Context, t *testing.T, store domain.AdminMarkets, token string, feedID int64) {
	t.Helper()
	value, err := store.Detail(ctx, token, feedID)
	if err != nil || value.Snapshot == nil || value.Snapshot.PublisherCurrent || value.Snapshot.Reading.Quality != domain.MarketQualityUnavailable || value.Snapshot.Reading.Reason != domain.MarketReasonDisconnected || !value.Snapshot.Reading.Bid.IsZero() || !value.Snapshot.Reading.Ask.IsZero() || !value.Snapshot.Reading.Mid.IsZero() || !value.Snapshot.Reading.BidQuantity.IsZero() || !value.Snapshot.Reading.AskQuantity.IsZero() {
		t.Fatal("lost publisher retained actionable market prices", err)
	}
}

func adminMarketsStore(t *testing.T, db *gorm.DB) domain.AdminMarkets {
	t.Helper()
	store, err := repository.NewAdminMarkets(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func adminMarketFilter() domain.AdminMarketFilter {
	return domain.AdminMarketFilter{
		Page: domain.AdminPage{Limit: 1},
		Mode: domain.MarketModeReplay,
	}
}

func adminMarketStoredSnapshot(ctx context.Context, t *testing.T, db *gorm.DB, feedID int64) *modelv1.MarketSnapshot {
	t.Helper()
	var row modelv1.MarketSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ?", feedID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}
