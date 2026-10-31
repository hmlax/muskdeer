//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminOverviewCountersAndPageShareAuthorizedRepeatableSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	const callback = "test:overview-between-count-and-page"
	created := false
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		_, strategies := query.Statement.Dest.(*[]*modelv1.StrategyInstance)
		if !strategies || created {
			return
		}
		created = true
		if _, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); err != nil {
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
	store := adminOverviewStore(t, db)
	filter := domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 10}}
	first, err := store.Strategies(ctx, actor.Token, filter)
	if err != nil || !created || first.Total != 1 || len(first.Items) != 1 || first.Items[0].Strategy.Spec.ID != submit.Input.Authority.StrategyID {
		t.Fatal("new strategy escaped the existing counter/authority snapshot", err)
	}
	next, err := store.Strategies(ctx, actor.Token, filter)
	if err != nil || next.Total != 2 || len(next.Items) != 2 || next.RequestedRunning != 1 {
		t.Fatal("next overview lost committed metadata", err)
	}
}

func TestAdminOverviewFailuresCancellationAndRevokedViewerReturnNoCounters(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, _ := adminCreationFixture(ctx, t, db)
	const callback = "test:overview-private-page-failure"
	injected := errors.New("private strategy overview page read failure")
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, strategies := query.Statement.Dest.(*[]*modelv1.StrategyInstance); strategies {
			_ = query.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	store := adminOverviewStore(t, db)
	filter := domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 1}}
	value, err := store.Strategies(ctx, actor.Token, filter)
	if !errors.Is(err, injected) || !errors.Is(err, domain.ErrAdminUnavailable) || err.Error() != domain.MessageAdminUnavailable || value.Total != 0 || value.Items != nil || value.StoredStates != nil || !value.ObservedAt.IsZero() {
		t.Fatal("failed page leaked counters, time or private details", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if value, err := store.Strategies(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) || value.Items != nil {
		t.Fatal("overview ignored parent cancellation", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Strategies(ctx, actor.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || value.Items != nil || value.Total != 0 {
		t.Fatal("revoked viewer obtained overview data", err)
	}
}

func TestAdminOverviewReadOnlyTransactionCannotDisableFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	const callback = "test:overview-read-only-funds"
	attempted := false
	var writeErr error
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, strategies := query.Statement.Dest.(*[]*modelv1.StrategyInstance)
		if !strategies || attempted {
			return
		}
		attempted = true
		writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.SimulatorAccount{}).
			Where("account_id = ?", submit.Input.AccountID).Update("enabled", false).Error
		_ = query.AddError(writeErr)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	value, err := adminOverviewStore(t, db).Strategies(ctx, actor.Token, domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 1}})
	if !attempted || !errors.Is(err, domain.ErrAdminUnavailable) || value.Items != nil || value.Total != 0 {
		t.Fatal("overview allowed funds mutation or partial counters", err)
	}
	assertDatabaseCode(t, writeErr, "25006")
	var account modelv1.SimulatorAccount
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ?", submit.Input.AccountID).Take(&account).Error; err != nil || !account.Enabled {
		t.Fatal("overview changed account enablement", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("overview changed stored runtime")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func adminOverviewStore(t *testing.T, db *gorm.DB) domain.AdminOverview {
	t.Helper()
	store, err := repository.NewAdminOverview(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
