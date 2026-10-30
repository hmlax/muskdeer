//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminTradeBrowserReadsAuthoritativeFillsWithoutRepairingPositions(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "trades-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	publishOrderHTTPSource(ctx, t, db, actor, submit)
	// Model a portfolio node whose recording clock is behind the execution
	// node. Persist the real fact normally; never edit its immutable timestamp.
	portfolioNodeTime := time.Now().Add(-time.Minute)
	portfolio, err := repository.NewPortfolio(db.Session(&gorm.Session{
		NowFunc: func() time.Time { return portfolioNodeTime },
	}))
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell)); err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatal("browser fixture did not retain incomplete collected facts", err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	observed, err := adminTradesStore(t, db).Detail(ctx, actor.Token, before.Id, sell.AggregateId)
	if err != nil || observed.PortfolioFact == nil || !observed.PortfolioFact.RecordedAt.Before(observed.RecordedAt) {
		t.Fatal("fixture failed to persist a valid collected fact with an independent earlier clock", err)
	}
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	runTradeBrowser(ctx, t, base, before.Id, other.StrategyID, buy.AggregateId, sell.AggregateId, buy.Fill.IntentId)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("browser reads changed runtime or repaired incomplete positions")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
}

func runTradeBrowser(ctx context.Context, t *testing.T, base string, strategyID, otherID, buyID, sellID, orderID int64) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-trades-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-trades")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_TRADE_STRATEGY="+strconv.FormatInt(strategyID, 10), "MUSKDEER_BROWSER_TRADE_OTHER="+strconv.FormatInt(otherID, 10),
		"MUSKDEER_BROWSER_TRADE_BUY="+strconv.FormatInt(buyID, 10), "MUSKDEER_BROWSER_TRADE_SELL="+strconv.FormatInt(sellID, 10), "MUSKDEER_BROWSER_TRADE_ORDER="+strconv.FormatInt(orderID, 10))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual trades browser failed: %v\n%s", err, output)
	}
	t.Logf("actual trades browser passed; screenshots=%s", artifacts)
}
