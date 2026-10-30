//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminTradesCoreAndCollectedFactShareOneRepeatableSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	buy, _ := portfolioTrades(ctx, t, core, intents, submit)
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:trade-concurrent-collection"
	collected := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, fills := query.Statement.Dest.(*[]*modelv1.SimulatorFill)
		if !fills || collected {
			return
		}
		collected = true
		if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, buy)); err != nil {
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
	store := adminTradesStore(t, db)
	page, err := store.List(ctx, actor.Token, adminTradeFilter(submit))
	if err != nil || !collected || len(page.Items) != 1 || page.Items[0].PortfolioFact != nil {
		t.Fatal("later portfolio collection escaped the original core snapshot", err)
	}
	next, err := store.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, buy.AggregateId)
	if err != nil || next.PortfolioFact == nil {
		t.Fatal("next snapshot failed to observe the committed collected fact", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func TestAdminTradesFailureCancellationAndRevokedAuthorityReturnNoPartialData(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	buy, _ := portfolioTrades(ctx, t, core, intents, submit)
	store := adminTradesStore(t, db)
	filter := adminTradeFilter(submit)
	injected := errors.New("private collected-fact query failure")
	const callback = "test:trade-collected-fact-failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, facts := query.Statement.Dest.(*[]*modelv1.PortfolioFill); facts {
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
		t.Fatal("failed related read leaked private details or a partial page", err)
	}
	if value, err := store.Detail(ctx, actor.Token, filter.StrategyID, buy.AggregateId); !errors.Is(err, domain.ErrAdminUnavailable) || value.ID != 0 {
		t.Fatal("failed related detail returned core-only success", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if page, err := store.List(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) || page.Items != nil {
		t.Fatal("trade reader lost parent cancellation", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if page, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || page.Items != nil {
		t.Fatal("revoked session read a trade page", err)
	}
	if value, err := store.Detail(ctx, actor.Token, filter.StrategyID, buy.AggregateId); !errors.Is(err, domain.ErrUnauthenticated) || value.ID != 0 {
		t.Fatal("revoked session read a trade detail", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
}

func TestAdminTradesDatabaseRejectsWritesInsideReadSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	portfolioTrades(ctx, t, core, intents, submit)
	const callback = "test:trade-read-only"
	attempted := false
	var writeErr error
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, fills := query.Statement.Dest.(*[]*modelv1.SimulatorFill)
		if !fills || attempted {
			return
		}
		attempted = true
		writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.Instrument{}).
			Where("id = ?", submit.Input.InstrumentID).Update("status", domain.InstrumentStatusHalted).Error
		_ = query.AddError(writeErr)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	page, err := adminTradesStore(t, db).List(ctx, actor.Token, adminTradeFilter(submit))
	if !attempted || !errors.Is(err, domain.ErrAdminUnavailable) || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("read snapshot allowed an injected mutation or partial page", err)
	}
	assertDatabaseCode(t, writeErr, "25006")
	var instrument modelv1.Instrument
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ?", submit.Input.InstrumentID).Take(&instrument).Error; err != nil || instrument.Status != domain.InstrumentStatusTrading {
		t.Fatal("failed trade read changed the instrument", err)
	}
}
