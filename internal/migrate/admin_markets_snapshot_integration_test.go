//go:build integration

package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminMarketsPublicationAndAuthorityShareOneRepeatableSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	feed, authority, snapshots := marketSnapshotFixture(ctx, t, db)
	store := adminMarketsStore(t, db)
	const callback = "test:admin-market-concurrent-publication"
	published := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, feeds := query.Statement.Dest.(*[]*modelv1.MarketFeed)
		if !feeds || published {
			return
		}
		published = true
		if _, err := snapshots.Publish(ctx, authority, 0, marketReadingFixture(feed, 1, "100")); err != nil {
			_ = query.AddError(err)
			return
		}
		if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", actor.Principal.UserID).
			Select("CredentialVersion", "Revision").Updates(&modelv1.AdminUser{
			CredentialVersion: 2,
			Revision:          2,
		}).Error; err != nil {
			_ = query.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	page, err := store.List(ctx, actor.Token, adminMarketFilter())
	if err != nil || !published || len(page.Items) != 1 || page.Items[0].Snapshot != nil {
		t.Fatal("publication after authorization escaped the original feed snapshot", err)
	}
	assertAdminMarketsUnauthenticated(ctx, t, store, actor.Token, feed.ID)
	observation, err := snapshots.Get(ctx, feed.ID)
	if err != nil || observation.Revision != 1 || observation.Reading.Quality != domain.MarketQualityHealthy {
		t.Fatal("concurrent authoritative publication was lost", err)
	}
}

func TestAdminMarketsReadOnlyFailureCancellationAndRevokedSession(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	feed, _, _ := marketSnapshotFixture(ctx, t, db)
	store := adminMarketsStore(t, db)
	assertAdminMarketsUnauthenticated(ctx, t, store, "invalid", feed.ID)
	assertAdminMarketsReadOnly(ctx, t, db, store, actor.Token, feed)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if result, err := store.List(canceled, actor.Token, adminMarketFilter()); !errors.Is(err, context.Canceled) || result.Items != nil {
		t.Fatal("market list lost parent cancellation or returned partial data", err)
	}
	if result, err := store.Detail(canceled, actor.Token, feed.ID); !errors.Is(err, context.Canceled) || result.Feed.ID != 0 {
		t.Fatal("market detail lost parent cancellation or returned partial data", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	assertAdminMarketsUnauthenticated(ctx, t, store, actor.Token, feed.ID)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
}

func assertAdminMarketsReadOnly(ctx context.Context, t *testing.T, db *gorm.DB, store domain.AdminMarkets, token string, feed domain.MarketFeedDefinition) {
	t.Helper()
	const callback = "test:admin-market-read-only"
	var writeErr error
	attempted := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, feeds := query.Statement.Dest.(*[]*modelv1.MarketFeed)
		if !feeds || attempted {
			return
		}
		attempted = true
		// Reset the completed SELECT after applying context; a later Session
		// call with Context would otherwise clone its existing SQL statement.
		writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.Instrument{}).
			Where("id = ?", feed.InstrumentID).Update("status", domain.InstrumentStatusHalted).Error
		_ = query.AddError(writeErr)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	result, err := store.List(ctx, token, adminMarketFilter())
	var databaseError *pgconn.PgError
	code := ""
	if errors.As(writeErr, &databaseError) {
		code = databaseError.Code
	}
	if !attempted || code != "25006" || !errors.Is(err, domain.ErrAdminUnavailable) || result.Items != nil || strings.Contains(err.Error(), "UPDATE") {
		t.Fatalf("market snapshot writable/leaked/partial: attempted=%t write=%T SQLSTATE=%s rows=%d public=%v", attempted, writeErr, code, len(result.Items), err)
	}
	var instrument modelv1.Instrument
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ?", feed.InstrumentID).Take(&instrument).Error; err != nil || instrument.Status != domain.InstrumentStatusTrading {
		t.Fatal("failed management read left a product mutation", err)
	}
}

func assertAdminMarketsUnauthenticated(ctx context.Context, t *testing.T, store domain.AdminMarkets, token string, feedID int64) {
	t.Helper()
	if result, err := store.List(ctx, token, adminMarketFilter()); !errors.Is(err, domain.ErrUnauthenticated) || result.Items != nil {
		t.Fatal("market list trusted invalid current authority", err)
	}
	if result, err := store.Detail(ctx, token, feedID); !errors.Is(err, domain.ErrUnauthenticated) || result.Feed.ID != 0 {
		t.Fatal("market detail trusted invalid current authority", err)
	}
}
