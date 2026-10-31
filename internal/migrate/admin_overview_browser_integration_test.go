//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminOverviewBrowserReadsStoredCountsLeaseFactsAndNamedReadinessWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "overview-viewer", domain.AdminRoleViewer, 121, domain.AdminSessionTTL)
	grid, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	maker, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	}
	token, err := leases.Acquire(ctx, key, submit.Input.Authority.OwnerID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token, err = leases.Renew(ctx, token, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	adminOverviewRecoverRunning(ctx, t, db, submit, token)
	if err := leases.Release(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Create(&modelv1.StrategyLease{StrategyInstanceId: grid.StrategyID}).Error; err != nil {
		t.Fatal(err)
	}
	before := []*modelv1.StrategyInstance{publicationInstance(ctx, t, db, key.StrategyID), publicationInstance(ctx, t, db, grid.StrategyID), publicationInstance(ctx, t, db, maker.StrategyID)}
	riskBefore := riskEventSourceRow(ctx, t, db, key.StrategyID)
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	a := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	a.InstanceId, a.Version = "overview-browser-a", "declared-example-a"
	a.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcessFor(ctx, t, a, discovery, 90*time.Second, "")
	b := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	b.InstanceId, b.Version = "overview-browser-b", "declared-example-b"
	b.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	bPID := startBusinessProcessFor(ctx, t, b, discovery, 90*time.Second, "")
	market := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceMarket))
	market.Business = nil // Registration only; no market worker or public-source claim.
	market.InstanceId = "overview-browser-market"
	startBusinessProcessFor(ctx, t, market, discovery, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, false, 90*time.Second)
	ids := [3]int64{key.StrategyID, grid.StrategyID, maker.StrategyID}
	runOverviewBrowserPhase(ctx, t, base, ids, submit.Input.AccountID, "healthy")
	if err := syscall.Kill(bPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitServiceObserverRemoval(ctx, t, discovery, b.InstanceId)
	runOverviewBrowserPhase(ctx, t, base, ids, submit.Input.AccountID, "removed")
	for _, row := range before {
		if !proto.Equal(row, publicationInstance(ctx, t, db, row.Id)) {
			t.Fatal("overview browser changed strategy runtime or requested target")
		}
	}
	if !proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, key.StrategyID)) || !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("overview browser changed risk or portfolio projection")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 2)
}

func runOverviewBrowserPhase(ctx context.Context, t *testing.T, base string, ids [3]int64, account, phase string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-overview-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, phase+"-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-overview")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_OVERVIEW_PHASE="+phase, "MUSKDEER_BROWSER_OVERVIEW_STRATEGY="+strconv.FormatInt(ids[0], 10),
		"MUSKDEER_BROWSER_OVERVIEW_GRID="+strconv.FormatInt(ids[1], 10), "MUSKDEER_BROWSER_OVERVIEW_MAKER="+strconv.FormatInt(ids[2], 10), "MUSKDEER_BROWSER_OVERVIEW_ACCOUNT="+account)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual overview browser %s failed: %v\n%s", phase, err, output)
	}
	t.Logf("actual overview browser %s passed; screenshots=%s", phase, artifacts)
}
