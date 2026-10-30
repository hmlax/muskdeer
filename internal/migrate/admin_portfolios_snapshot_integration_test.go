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

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminPortfoliosAuthorityProjectionSourceAndFundsShareRepeatableSnapshot(t *testing.T) {
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
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	const callback = "test:portfolio-concurrent-core-fill"
	filled := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, portfolios := query.Statement.Dest.(*[]*modelv1.PortfolioAccount)
		if !portfolios || filled {
			return
		}
		filled = true
		if _, err := core.Fill(ctx, domain.CoreFillRequest{
			AccountID:           submit.Input.AccountID,
			ClientOrderID:       live.ClientOrderID,
			TradeID:             "admin-portfolio-concurrent-fill",
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
	store := adminPortfoliosStore(t, db)
	page, err := store.List(ctx, actor.Token, domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 1}})
	if err != nil || !filled || len(page.Items) != 1 || page.Items[0].Source.Count != 0 || page.Items[0].CoreQuoteBalance == nil || page.Items[0].CoreQuoteBalance.Frozen.String() != "200.2" {
		t.Fatal("new core facts escaped the original projection snapshot", err)
	}
	next, err := store.Detail(ctx, actor.Token, projection.Id)
	if err != nil || next.Source.Count != 1 || next.Collected.Count != 0 || next.CoreQuoteBalance == nil || next.CoreQuoteBalance.Frozen.String() != "100.1" || !next.Projection.Reconciled || next.Projection.LastSourceSequence != 0 {
		t.Fatal("next snapshot lost current source/funds or overwrote stored diagnostics", err)
	}
	if !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("query reconciled an undelivered fill")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func TestAdminPortfoliosStorageFailureCancellationAndRevokedAuthorityReturnNothing(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	store := adminPortfoliosStore(t, db)
	row := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	filter := domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 1}}
	injected := errors.New("private core balance read failure")
	const callback = "test:portfolio-balance-read-failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, balances := query.Statement.Dest.(*[]*modelv1.SimulatorBalance); balances {
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
	page, err := store.List(ctx, actor.Token, filter)
	if !errors.Is(err, domain.ErrAdminUnavailable) || !errors.Is(err, injected) || err.Error() != domain.MessageAdminUnavailable || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("related-funds failure leaked a partial portfolio or private cause", err)
	}
	if value, err := store.Detail(ctx, actor.Token, row.Id); !errors.Is(err, domain.ErrAdminUnavailable) || value.ID != 0 {
		t.Fatal("failed funds lookup returned stored-only success", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if page, err := store.List(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) || page.Items != nil {
		t.Fatal("portfolio query lost parent cancellation", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if page, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || page.Items != nil {
		t.Fatal("revoked viewer read portfolios", err)
	}
	if value, err := store.Detail(ctx, actor.Token, row.Id); !errors.Is(err, domain.ErrUnauthenticated) || value.ID != 0 {
		t.Fatal("revoked viewer read a portfolio detail", err)
	}
}

func TestAdminPortfoliosReadSnapshotCannotWriteFundsOrReconciliation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	before := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	const callback = "test:portfolio-read-only"
	attempted := false
	var writeErr error
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, portfolios := query.Statement.Dest.(*[]*modelv1.PortfolioAccount)
		if !portfolios || attempted {
			return
		}
		attempted = true
		writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.PortfolioAccount{}).
			Where("id = ?", before.Id).Update("reconciled", false).Error
		_ = query.AddError(writeErr)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	page, err := adminPortfoliosStore(t, db).List(ctx, actor.Token, domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 1}})
	if !attempted || !errors.Is(err, domain.ErrAdminUnavailable) || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("portfolio read permitted mutation or partial data", err)
	}
	assertDatabaseCode(t, writeErr, "25006")
	if !proto.Equal(before, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("read changed stored reconciliation")
	}
}
