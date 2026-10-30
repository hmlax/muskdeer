//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminPortfoliosPageExactAccountsAndKeepMissingCoreBalancesAbsent(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); err != nil {
		t.Fatal(err)
	}
	other := adminPortfolioWithoutCore(ctx, t, db, submit.Input.InstrumentID)
	core, intents := executionFixture(t, db)
	portfolioTrades(ctx, t, core, intents, submit)
	store := adminPortfoliosStore(t, db)
	filter := domain.AdminPortfolioFilter{Page: domain.AdminPage{Limit: 1}}
	first, err := store.List(ctx, actor.Token, filter)
	if err != nil || len(first.Items) != 1 || first.NextAfterID != first.Items[0].ID || first.Items[0].Projection.AccountID != submit.Input.AccountID {
		t.Fatal("account portfolio cursor did not start at the original identity", err)
	}
	filter.Page.AfterID = first.NextAfterID
	last, err := store.List(ctx, actor.Token, filter)
	if err != nil || len(last.Items) != 1 || last.NextAfterID != 0 || last.Items[0].ID != other.Id || last.Items[0].AccountEnabled || last.Items[0].CoreBaseBalance != nil || last.Items[0].CoreQuoteBalance != nil || last.Items[0].Projection.Values.RealizedPnlQuote.String() != "-0.123456789012345678901234567890123456" {
		t.Fatal("missing funds, disabled account or signed high precision PnL was fabricated", err)
	}
	filter.Page.AfterID, filter.AccountID = 0, other.AccountId
	only, err := store.List(ctx, actor.Token, filter)
	if err != nil || len(only.Items) != 1 || only.Items[0].ID != other.Id || only.NextAfterID != 0 {
		t.Fatal("exact business account filter leaked another portfolio", err)
	}
	filter.AccountID, filter.InstrumentID, filter.Page.Limit = "", submit.Input.InstrumentID, 10
	both, err := store.List(ctx, actor.Token, filter)
	if err != nil || len(both.Items) != 2 || both.Items[0].Source.Count != 2 || both.Items[1].Source.Count != 0 || both.Items[1].Collected.Count != 0 {
		t.Fatal("instrument filter duplicated shared-account strategies or source counts", err)
	}
	if !proto.Equal(other, adminTradePortfolioRow(ctx, t, db, other.AccountId)) {
		t.Fatal("query changed an incomplete account projection")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioAccount{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 2)
}

// This explicit incomplete persisted fixture has no core funding rows. Reads
// must expose that absence; they cannot provision zero ledger balances.
func adminPortfolioWithoutCore(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64) *modelv1.PortfolioAccount {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(&modelv1.SimulatorAccount{
		AccountId:    "portfolio-without-core",
		SubjectId:    "portfolio-test-subject",
		Enabled:      false,
		QuoteFeeRate: "0",
		Revision:     1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	row := &modelv1.PortfolioAccount{
		AccountId:        "portfolio-without-core",
		InstrumentId:     instrumentID,
		BaseAsset:        "BTC",
		QuoteAsset:       "USDT",
		OpeningQuantity:  "0",
		OpeningCashQuote: "0",
		OpeningCostQuote: "0",
		Quantity:         "0",
		CashQuote:        "0",
		CostQuote:        "0",
		RealizedPnlQuote: "-0.123456789012345678901234567890123456",
		FeesQuote:        "0",
		Revision:         1,
		ErrorCode:        domain.PortfolioErrorMismatch,
	}
	if err := db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return adminTradePortfolioRow(ctx, t, db, row.AccountId)
}
