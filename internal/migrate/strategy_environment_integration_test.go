//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyEnvironmentRetainsOwnAllocationButSubtractsOtherObligations(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	peer := admissionSibling(ctx, t, db, submit)
	raiseInventoryTestBudgets(ctx, t, db, submit.Input.AccountID)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatalf("live quote fixture: %+v, %v", live, err)
	}
	if _, err := intents.Accept(ctx, peer.Input, peer.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopeInstrument)).UpdateColumn("limit_notional", "450").Error; err != nil {
		t.Fatal(err)
	}
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := lifecycle.Instance(ctx, submit.Input.Authority.StrategyID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := repository.NewStrategyEnvironment(db)
	if err != nil {
		t.Fatal(err)
	}
	input, err := reader.Current(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !input.TotalNotional.Equal(decimal.NewFromInt(250)) || !input.MaximumOrderNotional.Equal(decimal.NewFromInt(300)) {
		t.Fatalf("shared budget lost own reserve or counted peer capacity: total=%s single=%s", input.TotalNotional, input.MaximumOrderNotional)
	}
	assertStrategyPlanningInput(t, input, submit)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	action, err := actions.Plan(ctx, submit.Input.Authority, journalSubmitPlan(submit, "environment-refresh"))
	if err != nil {
		t.Fatal(err)
	}
	input, err = reader.Current(ctx, spec)
	if err != nil || !input.LastRefresh.Equal(action.CreatedAt) {
		t.Fatalf("refresh did not use durable decision time: %s, %v", input.LastRefresh, err)
	}
	stale := spec
	stale.Revision++
	if _, err := reader.Current(ctx, stale); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("stale spec was accepted: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopePlatform)).UpdateColumn("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Current(ctx, spec); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("disabled global policy supplied a quote allocation: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "400")
	assertGatewayOrderCount(ctx, t, db, 1)
}

func assertStrategyPlanningInput(t *testing.T, input domain.StrategyEnvironmentView, submit domain.GatewaySubmit) {
	t.Helper()
	if input.Parameters.Maker == nil || input.Parameters.Template != domain.StrategyTemplateMarketMaker ||
		input.Symbol != "BTCUSDT" || input.BaseAsset != "BTC" || input.QuoteAsset != "USDT" ||
		!input.LocalBid.Equal(submit.Input.Price) || !input.LocalAsk.IsZero() || !input.QuoteFeeRate.Equal(decimal.RequireFromString("0.001")) ||
		input.ObservedAt.IsZero() || !input.LastRefresh.IsZero() {
		t.Fatalf("current typed planning input is incomplete: %+v", input)
	}
	if !input.Inventory.Equal(decimal.NewFromInt(10)) || !input.AvailableBase.Equal(decimal.NewFromInt(10)) || !input.AvailableQuote.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("own confirmed freeze was double-counted in complete-book planning: inventory=%s base=%s quote=%s", input.Inventory, input.AvailableBase, input.AvailableQuote)
	}
	if !input.MaximumBuyQuantity.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("another strategy's outstanding buy was omitted from worst-inventory planning: %s", input.MaximumBuyQuantity)
	}
}
