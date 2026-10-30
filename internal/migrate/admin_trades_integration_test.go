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

func TestAdminTradesReadCoreFactsHistoricalSourceAndExactOrderScope(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "trade-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	reader := adminTradesStore(t, db)
	filter := adminTradeFilter(submit)
	empty, err := reader.List(ctx, viewer.Token, filter)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty strategy fabricated trades", err)
	}
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	publishOrderHTTPSource(ctx, t, db, actor, submit)
	before := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	first, err := reader.List(ctx, viewer.Token, filter)
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != buy.AggregateId || first.NextAfterID != buy.AggregateId {
		t.Fatal("authoritative fill cursor failed", err)
	}
	entry := first.Items[0]
	if entry.Source != domain.MarketSourceBinance || entry.ConfigVersion != 1 || entry.OrderID == nil || *entry.OrderID != buy.Fill.IntentId || entry.PortfolioFact != nil || entry.ExecutionVenue != domain.ExecutionVenueSimulator || entry.Price.String() != "99" || entry.Quantity.String() != "1" || entry.QuoteNotional.String() != "99" || entry.FeeAmount.String() != "0.099" || entry.FeeAsset != "USDT" || entry.RecordedAt.UnixMilli() <= 0 {
		t.Fatal("trade read substituted history, rounded fees or invented a projection")
	}
	filter.Page.AfterID = first.NextAfterID
	second, err := reader.List(ctx, viewer.Token, filter)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != sell.AggregateId || second.NextAfterID != 0 || second.Items[0].FeeAmount.String() != "0.101" {
		t.Fatal("final trade page lost an independent sell fill", err)
	}
	assertAdminTradeScope(ctx, t, reader, viewer.Token, submit, other.StrategyID, entry)
	if !proto.Equal(before, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("read rebuilt or reconciled the untouched portfolio")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 0)
}

func assertAdminTradeScope(ctx context.Context, t *testing.T, reader domain.AdminTrades, token string, submit domain.GatewaySubmit, otherID int64, entry domain.AdminTrade) {
	t.Helper()
	filter := adminTradeFilter(submit)
	filter.OrderID = *entry.OrderID
	one, err := reader.List(ctx, token, filter)
	if err != nil || len(one.Items) != 1 || one.Items[0].ID != entry.ID || one.NextAfterID != 0 {
		t.Fatal("explicit original OMS order filter failed", err)
	}
	filter.Side = domain.SideSell
	if page, err := reader.List(ctx, token, filter); err != nil || len(page.Items) != 0 || page.Items == nil {
		t.Fatal("contradictory valid side filter escaped the selected order", err)
	}
	if _, err := reader.Detail(ctx, token, otherID, entry.ID); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("core fill detail crossed strategy scope", err)
	}
	filter.StrategyID, filter.Side = otherID, domain.SideUnspecified
	if _, err := reader.List(ctx, token, filter); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("foreign OMS order was accepted as a sibling filter", err)
	}
	value, err := reader.Detail(ctx, token, entry.StrategyID, entry.ID)
	if err != nil || value.OrderID == nil || *value.OrderID != *entry.OrderID {
		t.Fatal("core fill detail lost original submit identity", err)
	}
	*value.OrderID = -1
	again, err := reader.Detail(ctx, token, entry.StrategyID, entry.ID)
	if err != nil || again.OrderID == nil || *again.OrderID != *entry.OrderID {
		t.Fatal("caller mutation contaminated a subsequent read", err)
	}
}

func TestAdminTradesReportCollectedFactWithoutClaimingReconciliation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell))
	if err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatal("fixture failed to retain an out-of-order incomplete projection", err)
	}
	before := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	reader := adminTradesStore(t, db)
	value, err := reader.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, sell.AggregateId)
	if err != nil || value.PortfolioFact == nil || value.PortfolioFact.ID <= 0 || value.PortfolioFact.RecordedAt.UnixMilli() <= 0 {
		t.Fatal("collected sell fact was hidden by incomplete positions", err)
	}
	missing, err := reader.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, buy.AggregateId)
	if err != nil || missing.PortfolioFact != nil || !proto.Equal(before, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("trade read delivered a missing fill or repaired positions", err)
	}
	if complete, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, buy)); err != nil || !complete.Reconciled {
		t.Fatal("actual portfolio consumer failed to finish the projection", err)
	}
	filter := adminTradeFilter(submit)
	filter.Page.Limit = 10
	page, err := reader.List(ctx, actor.Token, filter)
	if err != nil || len(page.Items) != 2 || page.Items[0].PortfolioFact == nil || page.Items[1].PortfolioFact == nil {
		t.Fatal("actual collected facts did not become visible", err)
	}
}

func TestAdminTradesOriginalCommandDoesNotAdoptMatchingUnlinkedIntent(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Quantity = decimal.NewFromInt(1)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	intent, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	submit.ClientOrderID = intent.ClientOrderID
	submit.Input.IdempotencyKey = "unlinked-core-command"
	submit.IntentID = 0
	if _, err := core.Submit(ctx, submit); err != nil {
		t.Fatal(err)
	}
	fill, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       submit.ClientOrderID,
		TradeID:             "original-unlinked-fill",
		CounterpartySubject: "external-liquidity",
		Price:               submit.Input.Price,
		Quantity:            submit.Input.Quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := adminTradesStore(t, db)
	value, err := reader.Detail(ctx, actor.Token, submit.Input.Authority.StrategyID, fill.ID)
	if err != nil || value.OrderID != nil || value.PortfolioFact != nil {
		t.Fatal("trade read adopted an OMS intent by client ID", err)
	}
	filter := adminTradeFilter(submit)
	filter.OrderID = intent.ID
	if page, err := reader.List(ctx, actor.Token, filter); err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Fatal("OMS filter invented original command linkage", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "100")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
}

func adminTradesStore(t *testing.T, db *gorm.DB) domain.AdminTrades {
	t.Helper()
	store, err := repository.NewAdminTrades(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func adminTradeFilter(submit domain.GatewaySubmit) domain.AdminTradeFilter {
	return domain.AdminTradeFilter{
		StrategyID: submit.Input.Authority.StrategyID,
		Page: domain.AdminPage{
			Limit: 1,
		},
	}
}

func adminTradePortfolioRow(ctx context.Context, t *testing.T, db *gorm.DB, accountID string) *modelv1.PortfolioAccount {
	t.Helper()
	var row modelv1.PortfolioAccount
	if err := db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", accountID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}
