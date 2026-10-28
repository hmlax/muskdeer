//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminCatalogKeepsActualChoicesPrecisionAndCurrentViewerAuthority(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	catalog, err := repository.NewAdminCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	page := domain.AdminPage{Limit: 1}
	empty, err := catalog.Accounts(ctx, actor.Token, page)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty catalog fabricated an account", err)
	}
	assertCatalogAccounts(ctx, t, db, catalog, actor.Token)
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instruments, err := catalog.Instruments(ctx, actor.Token, page)
	if err != nil || len(instruments.Items) != 1 || instruments.Items[0].Rules.ID != instrument.Id || instruments.Items[0].Symbol != instrument.Symbol || !instruments.Items[0].Rules.MinNotional.Equal(decimal.RequireFromString(instrument.MinNotional)) {
		t.Fatal("execution metadata lost exact decimal or identity", err)
	}
	feeds := catalogFeedFixtures(ctx, t, db, instrument.Id)
	bindings, err := catalog.Feeds(ctx, actor.Token, instrument.Id, domain.MarketModeReplay, page)
	if err != nil || len(bindings.Items) != 1 || bindings.Items[0].Source != domain.MarketSourceBinance || bindings.NextAfterID != feeds[0].ID {
		t.Fatal("feed catalog lost selected source or pagination", err)
	}
	bindings, err = catalog.Feeds(ctx, actor.Token, instrument.Id, domain.MarketModeReplay, domain.AdminPage{
		AfterID: bindings.NextAfterID,
		Limit:   1,
	})
	if err != nil || len(bindings.Items) != 1 || bindings.Items[0].Source != domain.MarketSourceOKX || bindings.Items[0].Symbol != "BTC-USDT" || bindings.NextAfterID != 0 {
		t.Fatal("feed catalog mixed or substituted external symbols", err)
	}
	live, err := catalog.Feeds(ctx, actor.Token, instrument.Id, domain.MarketModeLive, page)
	if err != nil || live.Items == nil || len(live.Items) != 0 {
		t.Fatal("REPLAY binding became a LIVE choice", err)
	}
	if _, err := catalog.Feeds(ctx, actor.Token, instrument.Id+1, domain.MarketModeReplay, page); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing instrument became an empty catalog", err)
	}
	if _, err := catalog.Accounts(ctx, "invalid", page); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("catalog accepted invalid authority", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	assertCatalogAuthorityDenied(ctx, t, catalog, actor.Token, instrument.Id)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketSnapshot{}, 0)
}

func assertCatalogAccounts(ctx context.Context, t *testing.T, db *gorm.DB, catalog domain.AdminCatalog, token string) {
	t.Helper()
	page := domain.AdminPage{Limit: 1}
	accounts := []*modelv1.SimulatorAccount{
		{
			AccountId:    "catalog-enabled",
			SubjectId:    "catalog-subject",
			Enabled:      true,
			QuoteFeeRate: "0.123456789012345678",
			Revision:     1,
		},
		{
			AccountId:    "catalog-disabled",
			SubjectId:    "catalog-subject",
			Enabled:      false,
			QuoteFeeRate: "0",
			Revision:     1,
		},
	}
	for _, row := range accounts {
		if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, err := catalog.Accounts(ctx, token, page)
	if err != nil || len(first.Items) != 1 || first.NextAfterID != accounts[0].Id || !first.Items[0].Enabled || !first.Items[0].QuoteFeeRate.Equal(decimal.RequireFromString(accounts[0].QuoteFeeRate)) {
		t.Fatal("account catalog lost identity, precision or bounds", err)
	}
	second, err := catalog.Accounts(ctx, token, domain.AdminPage{
		AfterID: first.NextAfterID,
		Limit:   1,
	})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != accounts[1].Id || second.Items[0].Enabled || !second.Items[0].QuoteFeeRate.IsZero() || second.NextAfterID != 0 {
		t.Fatal("disabled account was hidden or granted enablement", err)
	}
}

func catalogFeedFixtures(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64) []domain.MarketFeedDefinition {
	t.Helper()
	store, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	var feeds []domain.MarketFeedDefinition
	for _, binding := range []struct {
		source domain.MarketSource
		symbol string
	}{
		{
			source: domain.MarketSourceBinance,
			symbol: "BTCUSDT",
		},
		{
			source: domain.MarketSourceOKX,
			symbol: "BTC-USDT",
		},
	} {
		feed, err := store.Create(ctx, domain.MarketFeedDefinition{
			Source:        binding.source,
			InstrumentID:  instrumentID,
			Symbol:        binding.symbol,
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Policy:        marketquality.DefaultPolicy(),
		})
		if err != nil {
			t.Fatal(err)
		}
		feeds = append(feeds, feed)
	}
	return feeds
}

func assertCatalogAuthorityDenied(ctx context.Context, t *testing.T, catalog domain.AdminCatalog, token string, instrumentID int64) {
	t.Helper()
	page := domain.AdminPage{Limit: 1}
	if _, err := catalog.Accounts(ctx, token, page); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("account catalog trusted a revoked session", err)
	}
	if _, err := catalog.Instruments(ctx, token, page); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("instrument catalog trusted a revoked session", err)
	}
	if _, err := catalog.Feeds(ctx, token, instrumentID, domain.MarketModeReplay, page); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("feed catalog trusted a revoked session", err)
	}
}

func TestAdminCatalogFeedBindingAndInstrumentShareOneSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixture(ctx, t, db)
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	feeds := catalogFeedFixtures(ctx, t, db, instrument.Id)
	catalog, err := repository.NewAdminCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:catalog-concurrent-binding"
	inserted := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		row, ok := query.Statement.Dest.(*modelv1.Instrument)
		if !ok || row.Id != instrument.Id || inserted {
			return
		}
		inserted = true
		writer, err := repository.NewMarketFeeds(db.Session(&gorm.Session{NewDB: true}))
		if err != nil {
			_ = query.AddError(err)
			return
		}
		live := feeds[0]
		live.ID, live.Mode = 0, domain.MarketModeLive
		if _, err := writer.Create(ctx, live); err != nil {
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
	page := domain.AdminPage{Limit: 1}
	before, err := catalog.Feeds(ctx, actor.Token, instrument.Id, domain.MarketModeLive, page)
	if err != nil || !inserted || len(before.Items) != 0 {
		t.Fatal("concurrent binding escaped the instrument snapshot", err)
	}
	after, err := catalog.Feeds(ctx, actor.Token, instrument.Id, domain.MarketModeLive, page)
	if err != nil || len(after.Items) != 1 || after.Items[0].Mode != domain.MarketModeLive || after.Items[0].Source != domain.MarketSourceBinance {
		t.Fatal("next catalog snapshot lost the committed binding", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", actor.Principal.UserID).
		Select("CredentialVersion", "Revision").Updates(&modelv1.AdminUser{
		CredentialVersion: 2,
		Revision:          2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	assertCatalogAuthorityDenied(ctx, t, catalog, actor.Token, instrument.Id)
}
