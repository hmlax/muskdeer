//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/marketwire"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestFencedMarketSnapshotsKeepExactBaselineAcrossTakeoverAndExpiry(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feed, authority, snapshots := marketSnapshotFixture(ctx, t, db)
	if _, err := snapshots.Get(ctx, feed.ID); !errors.Is(err, domain.ErrMarketMissing) {
		t.Fatalf("unpublished stream returned data: %v", err)
	}
	reading := marketReadingFixture(feed, 10, "100")
	reading.Bid = decimal.RequireFromString("99.999999999999999999")
	reading.Ask = decimal.NewFromInt(100)
	reading.Mid = decimal.RequireFromString("99.9999999999999999995")
	first, err := snapshots.Publish(ctx, authority, 0, reading)
	if err != nil || first.Revision != 1 || first.Reading.Quality != domain.MarketQualityHealthy {
		t.Fatalf("initial publication: %+v, %v", first, err)
	}
	saved, err := snapshots.Get(ctx, feed.ID)
	if err != nil || saved.Reading.Mid.String() != "99.9999999999999999995" || !saved.Baseline.Mid.Equal(saved.Reading.Mid) {
		t.Fatalf("database rounded reference/baseline: %+v, %v", saved, err)
	}
	if _, err := snapshots.Publish(ctx, authority, 0, reading); !errors.Is(err, domain.ErrMarketRevision) {
		t.Fatalf("old durable revision overwrote snapshot: %v", err)
	}
	changed := marketReadingFixture(feed, 10, "100.01")
	if _, err := snapshots.Publish(ctx, authority, 1, changed); !errors.Is(err, domain.ErrOldMarketReading) {
		t.Fatalf("same native revision changed price: %v", err)
	}
	foreign := reading
	foreign.Source = domain.MarketSourceOKX
	if _, err := snapshots.Publish(ctx, authority, 1, foreign); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatalf("foreign source replaced selected stream: %v", err)
	}
	old := marketReadingFixture(feed, 9, "100")
	if _, err := snapshots.Publish(ctx, authority, 1, old); !errors.Is(err, domain.ErrOldMarketReading) {
		t.Fatalf("old sequence overwrote snapshot: %v", err)
	}
	timer := time.NewTimer(time.Until(first.Reading.ValidUntil) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("market snapshot expiry test exceeded deadline")
	case <-timer.C:
	}
	ageExpired, err := snapshots.Get(ctx, feed.ID)
	if err != nil || ageExpired.Reading.Quality != domain.MarketQualityStale || !ageExpired.Reading.Mid.IsZero() || !ageExpired.AuthorityUntil.After(time.Now()) {
		t.Fatalf("stored healthy quote outlived its freshness while publisher remained valid: %+v, %v", ageExpired, err)
	}
	queued := marketReadingFixture(feed, 11, "100")
	queued.EventTime = time.Now().Add(-3 * time.Second)
	stale, err := snapshots.Publish(ctx, authority, 1, queued)
	if err != nil || stale.Reading.Quality != domain.MarketQualityStale || !stale.Reading.Mid.IsZero() || !stale.Baseline.Mid.Equal(first.Baseline.Mid) {
		t.Fatalf("DB-time queue age or baseline changed: %+v, %v", stale, err)
	}
	assertMarketTakeover(ctx, t, db, snapshots, feed, authority, stale, first.Baseline)
}

func assertMarketTakeover(ctx context.Context, t *testing.T, db *gorm.DB, snapshots domain.MarketRepository, feed domain.MarketFeedDefinition, authority domain.LeaseToken, stale domain.MarketPublication, baseline domain.MarketBaseline) {
	t.Helper()
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Release(ctx, authority); err != nil {
		t.Fatal(err)
	}
	nextOwner, err := ownerid.New(ctx, "market-takeover")
	if err != nil {
		t.Fatal(err)
	}
	next, err := leases.Acquire(ctx, authority.Key, nextOwner, 5*time.Second)
	if err != nil || next.Epoch != authority.Epoch+1 {
		t.Fatalf("takeover: %+v, %v", next, err)
	}
	if _, err := snapshots.Publish(ctx, authority, stale.Revision, marketReadingFixture(feed, 12, "100")); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("old publisher wrote after takeover: %v", err)
	}
	jump, err := snapshots.Publish(ctx, next, stale.Revision, marketReadingFixture(feed, 1, "200"))
	if err != nil || jump.Reading.Quality != domain.MarketQualityOutlier || !jump.Reading.Mid.IsZero() || !jump.Baseline.Mid.Equal(baseline.Mid) {
		t.Fatalf("process restart bypassed persisted baseline: %+v, %v", jump, err)
	}
	restarted, err := marketquality.New(feed.Source, marketwire.Instrument{
		ID:     feed.InstrumentID,
		Symbol: feed.Symbol,
	}, feed.Mode, feed.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(jump.Baseline, time.Now()); err != nil {
		t.Fatalf("durable baseline could not restore tracker: %v", err)
	}
	recovered, err := snapshots.Publish(ctx, next, jump.Revision, marketReadingFixture(feed, 2, "100"))
	if err != nil || recovered.Reading.Quality != domain.MarketQualityHealthy {
		t.Fatalf("valid next-epoch reference rejected: %+v, %v", recovered, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).Update("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	expired, err := snapshots.Get(ctx, feed.ID)
	if err != nil || expired.Reading.Quality != domain.MarketQualityUnavailable || !expired.Reading.Mid.IsZero() || expired.Baseline.Mid.String() != "100" {
		t.Fatalf("reader trusted a persisted HEALTHY after authority expired: %+v, %v", expired, err)
	}
	next.ExpiresAt = time.Now().Add(time.Hour)
	if _, err := snapshots.Publish(ctx, next, recovered.Revision, marketReadingFixture(feed, 3, "100")); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("forged client expiry authorized publication: %v", err)
	}
}

func TestMarketSnapshotCommitFenceRollsBackExpiredPublisherAndBaseline(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feed, token, _ := marketSnapshotFixture(ctx, t, db)
	var row *modelv1.MarketSnapshot
	err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Transaction(func(tx *gorm.DB) error {
		row = rawMarketSnapshot(feed, token)
		if err := tx.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Create(row).Error; err != nil {
			return err
		}
		// Authorization changes after the write but before deferred validation.
		return tx.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).Update("expires_at", 1).Error
	})
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.ConstraintName != "market_snapshot_fence" {
		t.Fatalf("final target did not revalidate authority: %v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ?", feed.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected publication left quote/baseline: count=%d, err=%v", count, err)
	}
	row = rawMarketSnapshot(feed, token)
	row.MidPrice = "NaN"
	if err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Create(row).Error; err == nil {
		t.Fatal("non-finite market reference reached storage")
	}
	badPolicy := &modelv1.MarketFeed{
		Source:         string(domain.MarketSourceOKX),
		InstrumentId:   feed.InstrumentID,
		ExternalSymbol: "BTC-USDT",
		Mode:           string(domain.MarketModeLive),
		SourceVersion:  domain.MarketSourceVersion,
		Policy:         &commonv1.MarketPolicy{},
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Create(badPolicy).Error; err == nil {
		t.Fatal("NULL SQL check semantics accepted missing policy fields")
	}
}

func marketSnapshotFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.MarketFeedDefinition, domain.LeaseToken, domain.MarketRepository) {
	t.Helper()
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := feeds.Create(ctx, domain.MarketFeedDefinition{
		Source:        domain.MarketSourceBinance,
		InstrumentID:  marketInstrumentFixture(ctx, t, db),
		Symbol:        "BTCUSDT",
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
	owner, err := ownerid.New(ctx, "market-publisher")
	if err != nil {
		t.Fatal(err)
	}
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feed.ID,
	}, owner, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		t.Fatal(err)
	}
	return feed, token, snapshots
}

func marketReadingFixture(feed domain.MarketFeedDefinition, sequence int64, mid string) domain.MarketReading {
	now := time.Now().UTC().Truncate(time.Millisecond)
	price := decimal.RequireFromString(mid)
	return domain.MarketReading{
		Source:        feed.Source,
		InstrumentID:  feed.InstrumentID,
		Symbol:        feed.Symbol,
		Mode:          feed.Mode,
		SourceVersion: feed.SourceVersion,
		Connection:    1,
		Generation:    1,
		Sequence:      sequence,
		Quality:       domain.MarketQualityHealthy,
		Reason:        domain.MarketReasonNone,
		Bid:           price.Sub(decimal.RequireFromString("0.01")),
		BidQuantity:   decimal.NewFromInt(1),
		Ask:           price.Add(decimal.RequireFromString("0.01")),
		AskQuantity:   decimal.NewFromInt(1),
		Mid:           price,
		EventTime:     now.Add(-10 * time.Millisecond),
		ReceivedAt:    now,
		EvaluatedAt:   now,
		ValidUntil:    now.Add(time.Second),
	}
}

func rawMarketSnapshot(feed domain.MarketFeedDefinition, token domain.LeaseToken) *modelv1.MarketSnapshot {
	reading := marketReadingFixture(feed, 1, "100")
	return &modelv1.MarketSnapshot{
		MarketFeedId:     feed.ID,
		PublisherOwnerId: token.OwnerID,
		PublisherEpoch:   token.Epoch,
		Revision:         1,
		Connection:       1,
		Generation:       1,
		Sequence:         1,
		Quality:          string(reading.Quality),
		Reason:           string(reading.Reason),
		BidPrice:         reading.Bid.String(),
		BidQuantity:      "1",
		AskPrice:         reading.Ask.String(),
		AskQuantity:      "1",
		MidPrice:         reading.Mid.String(),
		EventAt:          reading.EventTime.UnixMilli(),
		ReceivedAt:       reading.ReceivedAt.UnixMilli(),
		EvaluatedAt:      reading.EvaluatedAt.UnixMilli(),
		ValidUntil:       reading.ValidUntil.UnixMilli(),
		BaselineMid:      reading.Mid.String(),
		BaselineAt:       reading.EvaluatedAt.UnixMilli(),
	}
}
