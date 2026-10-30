//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminTradesPreservePartialFillFeesBeyondEighteenPlaces(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ?", submit.Input.AccountID).
		Select("QuoteFeeRate", "Revision").Updates(&modelv1.SimulatorAccount{
		QuoteFeeRate: "0.001234567890123456",
		Revision:     2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	if grant, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID); err != nil || !grant.Allowed {
		t.Fatal("actual fee change failed to produce a current risk snapshot", err)
	}
	core, intents := executionFixture(t, db)
	submit.Input.Price = decimal.RequireFromString("99.99")
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	for _, tradeID := range []string{"exact-fee-first", "exact-fee-second"} {
		if _, err := core.Fill(ctx, domain.CoreFillRequest{
			AccountID:           submit.Input.AccountID,
			ClientOrderID:       live.ClientOrderID,
			TradeID:             tradeID,
			CounterpartySubject: "external-liquidity",
			Price:               submit.Input.Price,
			Quantity:            decimal.NewFromInt(1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	filter := adminTradeFilter(submit)
	filter.Page.Limit = 10
	page, err := adminTradesStore(t, db).List(ctx, actor.Token, filter)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("multiple fills for one core order disappeared", err)
	}
	for _, value := range page.Items {
		if value.Price.String() != "99.99" || value.Quantity.String() != "1" || value.QuoteNotional.String() != "99.99" || value.FeeAmount.String() != "0.12344444333344436544" || value.OrderID == nil || *value.OrderID != live.ID {
			t.Fatal("actual partial-fill precision or original association changed")
		}
	}
	*page.Items[0].OrderID = -1
	if *page.Items[1].OrderID != live.ID {
		t.Fatal("partial fills shared a mutable optional identity pointer")
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.77311111333311126912", "0")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "199.98")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
}
