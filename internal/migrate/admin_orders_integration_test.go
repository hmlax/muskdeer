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
	"github.com/hmlax/muskdeer/internal/simulator"
)

func TestAdminOrdersReadActualOMSAndCoreWithoutReconcilingUncertainty(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "order-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	reader, err := repository.NewAdminOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.AdminOrderFilter{
		StrategyID: submit.Input.Authority.StrategyID,
		Page:       domain.AdminPage{Limit: 1},
	}
	empty, err := reader.List(ctx, viewer.Token, filter)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty strategy fabricated orders", err)
	}
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	live, unsent, unknown := adminOrderFacts(ctx, t, db, core, intents, submit)
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "admin-order-unconfirmed-partial-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, filter.StrategyID)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, publication); err != nil {
		t.Fatal(err)
	}
	entries := readAdminOrderPages(ctx, t, reader, viewer.Token, filter)
	if len(entries) != 3 || entries[0].ID != live.ID || entries[1].ID != unsent.ID || entries[2].ID != unknown.ID {
		t.Fatal("order identity cursor lost or repeated records")
	}
	for _, entry := range entries {
		if entry.Source != domain.MarketSourceBinance || entry.ConfigVersion != 1 || entry.AccountID != submit.Input.AccountID || entry.InstrumentID != submit.Input.InstrumentID {
			t.Fatal("order read used the newly requested source or crossed scope")
		}
	}
	assertAdminOrderObservations(ctx, t, reader, viewer, filter.StrategyID, live, unsent, unknown)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "789.79", "110.11")
	filter.Page.AfterID, filter.State, filter.OpenOnly = 0, domain.OrderStateUnknown, true
	page, err := reader.List(ctx, viewer.Token, filter)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != unknown.ID || page.NextAfterID != 0 {
		t.Fatal("unknown order state filter hid an obligation", err)
	}
	if _, err := reader.Detail(ctx, viewer.Token, other.StrategyID, live.ID); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("order detail crossed into a sibling strategy", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 3)
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
}

func adminOrderFacts(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, intents domain.IntentExecutionRepository, submit domain.GatewaySubmit) (domain.IntentSnapshot, domain.IntentSnapshot, domain.IntentSnapshot) {
	t.Helper()
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatal("order query fixture did not fund a live order", err)
	}
	small := submit.Input
	small.IdempotencyKey = "admin-order-unsent-0001"
	small.Quantity = decimal.RequireFromString("0.1")
	small.Deadline = time.Now().Add(time.Second)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	unsent, err := intents.Accept(ctx, small, submit.ExecutionAuthority)
	if err != nil || unsent.State != domain.OrderStateIntent {
		t.Fatal(err)
	}
	lost, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit: simulator.ResponseLost,
		Cancel: simulator.ResponseNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	small.IdempotencyKey = "admin-order-unknown-0001"
	small.Deadline = time.Now().Add(time.Second)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	unknown, err := intentServiceFixture(t, intents, lost).Submit(ctx, small, submit.ExecutionAuthority)
	if err != nil || unknown.State != domain.OrderStateUnknown {
		t.Fatal(err)
	}
	return live, unsent, unknown
}

func readAdminOrderPages(ctx context.Context, t *testing.T, reader domain.AdminOrders, token string, filter domain.AdminOrderFilter) []domain.AdminOrder {
	t.Helper()
	var entries []domain.AdminOrder
	for {
		page, err := reader.List(ctx, token, filter)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID <= filter.Page.AfterID {
			t.Fatal("scoped order page failed", err)
		}
		entries = append(entries, page.Items...)
		if page.NextAfterID == 0 {
			return entries
		}
		if len(entries) >= 3 || page.NextAfterID != page.Items[0].ID {
			t.Fatal("order cursor did not advance on the last returned identity")
		}
		filter.Page.AfterID = page.NextAfterID
	}
}

func assertAdminOrderObservations(ctx context.Context, t *testing.T, reader domain.AdminOrders, actor domain.AdminLogin, strategyID int64, live, unsent, unknown domain.IntentSnapshot) {
	t.Helper()
	partial, err := reader.Detail(ctx, actor.Token, strategyID, live.ID)
	if err != nil || partial.State != domain.OrderStateLive || !partial.FilledQuantity.IsZero() || partial.ReservedNotional.String() != "200" || partial.Core == nil || partial.Core.State != domain.OrderStatePartiallyFilled || partial.Core.FilledQuantity.String() != "1" || partial.Core.FrozenAmount.String() != "100.1" || partial.ConfirmedCoreRevision >= partial.Core.Revision {
		t.Fatal("order read hid core fill progress or fabricated OMS reconciliation", err)
	}
	missing, err := reader.Detail(ctx, actor.Token, strategyID, unsent.ID)
	if err != nil || missing.Core != nil || missing.ConfirmedCoreID != 0 || missing.State != domain.OrderStateIntent || missing.ReservedNotional.String() != "10" {
		t.Fatal("unsubmitted order invented core acceptance or released funds", err)
	}
	uncertain, err := reader.Detail(ctx, actor.Token, strategyID, unknown.ID)
	if err != nil || uncertain.State != domain.OrderStateUnknown || uncertain.ErrorCode != domain.IntentErrorOutcomeUnknown || uncertain.ConfirmedCoreID != 0 || uncertain.Core == nil || uncertain.Core.State != domain.OrderStateLive || uncertain.ReservedNotional.String() != "10" || uncertain.Core.FrozenAmount.String() != "10.01" {
		t.Fatal("unknown order lost independently observed actual acceptance", err)
	}
}
