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
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminOrdersKeepAuthorityOMSAndCoreWithinOneReadSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := repository.NewAdminOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	inserted := false
	const callback = "test:order-core-after-authentication-snapshot"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, ok := query.Statement.Dest.(*modelv1.AdminSession); !ok || inserted {
			return
		}
		inserted = true
		if _, err := core.Fill(ctx, domain.CoreFillRequest{
			AccountID:           submit.Input.AccountID,
			ClientOrderID:       live.ClientOrderID,
			TradeID:             "admin-order-snapshot-concurrent-fill",
			CounterpartySubject: "external-liquidity",
			Price:               decimal.NewFromInt(100),
			Quantity:            decimal.NewFromInt(1),
		}); err != nil {
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
	before, err := reader.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, live.ID)
	if err != nil || !inserted || before.Core == nil || before.Core.State != domain.OrderStateLive || !before.Core.FilledQuantity.IsZero() || before.Core.FrozenAmount.String() != "200.2" {
		t.Fatal("core fill committed after authority lookup escaped the RR snapshot", err)
	}
	after, err := reader.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, live.ID)
	if err != nil || after.Core == nil || after.Core.State != domain.OrderStatePartiallyFilled || after.Core.FilledQuantity.String() != "1" || after.Core.FrozenAmount.String() != "100.1" || after.State != domain.OrderStateLive || !after.FilledQuantity.IsZero() || after.ReservedNotional.String() != "200" {
		t.Fatal("next read did not distinguish new core facts from unchanged OMS", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "100.1")
}

func TestAdminOrdersFailuresAndRevokedAuthorityReturnNoPartialData(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, _, _ := adminOrderFacts(ctx, t, db, core, intents, submit)
	reader, err := repository.NewAdminOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.AdminOrderFilter{
		StrategyID: submit.Input.Authority.StrategyID,
		Page:       domain.AdminPage{Limit: 1},
	}
	injected := errors.New("private core query storage failure")
	const callback = "test:reject-admin-order-core-read"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, ok := query.Statement.Dest.(*[]*modelv1.SimulatorOrder); ok {
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
	page, err := reader.List(ctx, actor.Token, filter)
	if !errors.Is(err, domain.ErrAdminUnavailable) || !errors.Is(err, injected) || err.Error() != domain.MessageAdminUnavailable || len(page.Items) != 0 || page.NextAfterID != 0 {
		t.Fatal("failed associated-core read leaked a page, cursor or private cause", err)
	}
	if detail, err := reader.Detail(ctx, actor.Token, filter.StrategyID, live.ID); !errors.Is(err, domain.ErrAdminUnavailable) || detail.ID != 0 || detail.Core != nil {
		t.Fatal("failed core detail returned partial OMS success", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if page, err := reader.List(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) || len(page.Items) != 0 || page.NextAfterID != 0 {
		t.Fatal("canceled reader returned partial data", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if page, err := reader.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || len(page.Items) != 0 {
		t.Fatal("revoked session read an order page", err)
	}
	if detail, err := reader.Detail(ctx, actor.Token, filter.StrategyID, live.ID); !errors.Is(err, domain.ErrUnauthenticated) || detail.ID != 0 {
		t.Fatal("revoked session read an order detail", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 3)
}
