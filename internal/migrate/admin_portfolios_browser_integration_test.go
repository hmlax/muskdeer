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

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminPortfolioBrowserSeparatesProjectionCoreFundsAndAccountScope(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "portfolios-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	other := adminPortfolioWithoutCore(ctx, t, db, submit.Input.InstrumentID)
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(&modelv1.SimulatorAccount{
		AccountId:    "portfolio-uninitialized",
		SubjectId:    "portfolio-browser-test",
		QuoteFeeRate: "0",
		Revision:     1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	_, sell := portfolioTrades(ctx, t, core, intents, submit)
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell)); err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatal("browser fixture did not preserve incomplete account positions", err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
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
	runPortfolioBrowser(ctx, t, base, before.Id, projection, other)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) || !proto.Equal(other, adminTradePortfolioRow(ctx, t, db, other.AccountId)) {
		t.Fatal("browser query changed shared strategy, stored positions or missing funds")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioAccount{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 2)
}

func runPortfolioBrowser(ctx context.Context, t *testing.T, base string, strategyID int64, primary, other *modelv1.PortfolioAccount) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-portfolios-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-portfolios")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_PORTFOLIO_PRIMARY="+strconv.FormatInt(primary.Id, 10), "MUSKDEER_BROWSER_PORTFOLIO_OTHER="+strconv.FormatInt(other.Id, 10),
		"MUSKDEER_BROWSER_PORTFOLIO_ACCOUNT="+primary.AccountId, "MUSKDEER_BROWSER_PORTFOLIO_INSTRUMENT="+strconv.FormatInt(primary.InstrumentId, 10),
		"MUSKDEER_BROWSER_PORTFOLIO_STRATEGY="+strconv.FormatInt(strategyID, 10))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual portfolio browser failed: %v\n%s", err, output)
	}
	t.Logf("actual portfolio browser passed; screenshots=%s", artifacts)
}
