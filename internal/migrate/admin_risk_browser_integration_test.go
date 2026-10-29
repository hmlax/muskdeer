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

const riskBrowserReason = `风险原因 <img src=x onerror="window.__riskExecuted=true"> 保留原文`

func TestAdminRiskBrowserPreservesFundsAndRequiresExplicitRecovery(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	riskActorFixture(ctx, t, db, "risk-viewer", domain.AdminRoleViewer, 71, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatal("risk browser did not establish a funded core order", err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	feed, _, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feed.ID,
	})
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
	startBusinessProcessFor(ctx, t, marketProcessConfig(t, ca, postgres, naming, feed.ID, "risk-browser-market", "99.99", "100.01"), discovery, 90*time.Second, "")
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcessFor(ctx, t, strategy, discovery, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, true, 90*time.Second)
	runRiskBrowserPhase(ctx, t, base, before, "block")
	control := riskControlFixture(t, db)
	assertRiskControlState(ctx, t, control, actor, before.Id, false, 2)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	renewRiskBrowserAuthority(ctx, t, db, submit)
	cancels := cancellationServiceFixture(t, db, intents, core)
	cancelCall, stopCancel := context.WithTimeout(ctx, 2*time.Second)
	pending, err := cancels.Cancel(cancelCall, cancelInputFixture(cancelCall, submit, live.ClientOrderID, "browser-risk-confirmed-cancel-01"), submit.ExecutionAuthority)
	stopCancel()
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal("risk browser could not retain an actual pending cancellation", err)
	}
	runRiskBrowserPhase(ctx, t, base, before, "pending")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	if result, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil || result.Request.State != domain.CancellationConfirmed {
		t.Fatal("risk browser did not observe the real core terminal state", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	runRiskBrowserPhase(ctx, t, base, before, "restore")
	runRiskBrowserPhase(ctx, t, base, before, "viewer")
	assertRiskControlState(ctx, t, control, actor, before.Id, true, 3)
	after := publicationInstance(ctx, t, db, before.Id)
	// The browser deliberately submits one distinct pause to invalidate the old
	// recovery confirmation. Risk controls cannot change any other field.
	expected := proto.CloneOf(before)
	expected.DesiredState = string(domain.StrategyStatePaused)
	expected.DesiredRunning = false
	expected.Revision++
	expected.UpdateAt = after.UpdateAt
	if !proto.Equal(expected, after) {
		t.Fatal("risk browser changed more than its explicit independent pause target")
	}
	assertRiskBrowserFinal(ctx, t, db, core, submit)
}

func renewRiskBrowserAuthority(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit) {
	t.Helper()
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	strategy := domain.LeaseToken{
		Key: domain.LeaseKey{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: submit.Input.Authority.StrategyID,
		},
		OwnerID: submit.Input.Authority.OwnerID,
		Epoch:   submit.Input.Authority.OwnerEpoch,
	}
	for _, token := range []domain.LeaseToken{strategy, submit.ExecutionAuthority} {
		if _, err := leases.Renew(ctx, token, 30*time.Second); err != nil {
			t.Fatal("browser fixture lost original fenced cancellation ownership", err)
		}
	}
}

func assertRiskBrowserFinal(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit) {
	t.Helper()
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	assertGatewayOrderCount(ctx, t, db, 1)
	var snapshot modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).Take(&snapshot).Error; err != nil || snapshot.AllowNewRisk {
		t.Fatal("risk browser opened a trading grant", err)
	}
	var changes []*modelv1.AdminStrategyRiskChange
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Order("revision ASC").Find(&changes).Error; err != nil || len(changes) != 2 || changes[0].Reason != riskBrowserReason || changes[1].ExpectedDesiredState == nil || *changes[1].ExpectedDesiredState != string(domain.StrategyStatePaused) {
		t.Fatal("browser did not preserve exact reason or newly confirmed pause target", err)
	}
}

func runRiskBrowserPhase(ctx context.Context, t *testing.T, base string, instance *modelv1.StrategyInstance, phase string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-risk-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-"+phase+"-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-risk")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_STRATEGY_ID="+strconv.FormatInt(instance.Id, 10), "MUSKDEER_BROWSER_STRATEGY_REVISION="+strconv.FormatInt(instance.Revision, 10),
		"MUSKDEER_BROWSER_RISK_PHASE="+phase, "MUSKDEER_BROWSER_RISK_REASON="+riskBrowserReason)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual risk browser failed: phase=%s error=%v\n%s", phase, err, output)
	}
	t.Logf("actual funded risk browser passed; phase=%s screenshots=%s", phase, artifacts)
}
