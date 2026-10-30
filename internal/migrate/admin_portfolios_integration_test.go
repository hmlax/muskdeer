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

func TestAdminPortfoliosKeepStoredDiagnosticSeparateFromUndeliveredCoreFills(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "portfolio-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	store := adminPortfoliosStore(t, db)
	filter := domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 10}}
	initial, err := store.List(ctx, viewer.Token, filter)
	if err != nil || len(initial.Items) != 1 || !initial.Items[0].Projection.Reconciled || initial.Items[0].Source.Count != 0 || initial.Items[0].Collected.Count != 0 {
		t.Fatal("initialized projection lost its stored diagnostic or invented fills", err)
	}
	core, intents := executionFixture(t, db)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	before := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	value := adminPortfolioUndelivered(ctx, t, store, viewer.Token, initial.Items[0].ID, sell.AggregateId)
	if !proto.Equal(before, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("query mutated a stored reconciliation flag")
	}
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell)); err != nil {
		t.Fatal(err)
	}
	incomplete := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	partial, err := store.Detail(ctx, actor.Token, value.ID)
	if err != nil || partial.Projection.Reconciled || partial.Projection.ErrorCode != domain.PortfolioErrorIncomplete || partial.Source.Count != 2 || partial.Collected.Count != 1 || partial.Collected.LatestSourceSequence != sell.AggregateId || !proto.Equal(incomplete, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("collected source count became fabricated complete positions", err)
	}
	complete, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, buy))
	if err != nil || !complete.Reconciled {
		t.Fatal(err)
	}
	final, err := store.Detail(ctx, viewer.Token, value.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertAdminPortfolioComplete(t, final, complete, submit)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
}

func adminPortfolioUndelivered(ctx context.Context, t *testing.T, store domain.AdminPortfolios, token string, id, latest int64) domain.AdminPortfolio {
	t.Helper()
	value, err := store.Detail(ctx, token, id)
	if err != nil || !value.Projection.Reconciled || value.Source.Count != 2 || value.Source.LatestSourceSequence != latest || value.Collected.Count != 0 || value.Projection.LastSourceSequence != 0 || value.Projection.Values.CashQuote.String() != "1000" || value.CoreQuoteBalance == nil || value.CoreQuoteBalance.Available.String() != "1001.8" || !value.CoreQuoteBalance.Frozen.IsZero() {
		t.Fatal("read hid source lag or silently reconciled the old stored projection", err)
	}
	return value
}

func assertAdminPortfolioComplete(t *testing.T, value domain.AdminPortfolio, complete domain.PortfolioSnapshot, submit domain.GatewaySubmit) {
	t.Helper()
	if !value.Projection.Reconciled || value.Collected.Count != 2 || value.Projection.Values.CashQuote.String() != "1001.8" || value.Projection.Values.FeesQuote.String() != "0.2" || !value.Projection.Values.RealizedPnlQuote.Equal(complete.Values.RealizedPnlQuote) || value.CoreBaseBalance == nil || value.CoreBaseBalance.Available.String() != "10" || value.Projection.AccountID != submit.Input.AccountID || value.Projection.InstrumentID != submit.Input.InstrumentID {
		t.Fatal("actual consumer progress lost account scope or exact portfolio values")
	}
}

func TestAdminPortfoliosMissingRowsDoNotCreateZeroBalancesOrPositions(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	store := adminPortfoliosStore(t, db)
	filter := domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 1}}
	empty, err := store.List(ctx, actor.Token, filter)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty database invented initialized positions", err)
	}
	if _, err := store.Detail(ctx, actor.Token, 1); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing projection became a zero portfolio", err)
	}
	filter.AccountID = "missing-account"
	if _, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("unknown account became an empty existing choice", err)
	}
	filter.AccountID, filter.InstrumentID = "", 1
	if _, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("unknown instrument became an empty existing choice", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioAccount{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
}

func adminPortfoliosStore(t *testing.T, db *gorm.DB) domain.AdminPortfolios {
	t.Helper()
	store, err := repository.NewAdminPortfolios(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
