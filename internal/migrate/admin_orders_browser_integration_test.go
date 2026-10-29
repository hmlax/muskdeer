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

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminOrderBrowserReadsActualFundedOrdersWithoutReconciliation(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "orders-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	live, unsent, unknown := adminOrderFacts(ctx, t, db, core, intents, submit)
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "admin-order-browser-partial-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	publishOrderHTTPSource(ctx, t, db, actor, submit)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	runOrderBrowser(ctx, t, base, before.Id, live.ID, unsent.ID, unknown.ID)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "789.79", "110.11")
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 3)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("browser order reads changed runtime authority or requested configuration")
	}
}

func runOrderBrowser(ctx context.Context, t *testing.T, base string, strategyID, liveID, unsentID, unknownID int64) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-orders-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-orders")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_ORDER_STRATEGY="+strconv.FormatInt(strategyID, 10), "MUSKDEER_BROWSER_ORDER_LIVE="+strconv.FormatInt(liveID, 10),
		"MUSKDEER_BROWSER_ORDER_UNSENT="+strconv.FormatInt(unsentID, 10), "MUSKDEER_BROWSER_ORDER_UNKNOWN="+strconv.FormatInt(unknownID, 10))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual orders browser failed: %v\n%s", err, output)
	}
	t.Logf("actual orders browser passed; screenshots=%s", artifacts)
}
