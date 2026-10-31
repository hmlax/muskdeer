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

func TestAdminRiskEventBrowserReadsHistoricalFlagsAndRemovedSourcesWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "risk-events-viewer", domain.AdminRoleViewer, 81, domain.AdminSessionTTL)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if value, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID); err != nil || !value.Allowed {
		t.Fatal("browser fixture did not reach actual risk readiness", err)
	}
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Fill(ctx, riskEventFillRequest(submit, live.ClientOrderID)); err != nil {
		t.Fatal(err)
	}
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).Delete(&modelv1.RiskSnapshot{}).Error; err != nil {
		t.Fatal(err)
	}
	history := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(history) != 4 {
		t.Fatal("browser fixture lacks immutable actual risk history")
	}
	other := adminRiskEventUnfundedSource(ctx, t, db)
	otherHistory := riskEventRows(ctx, t, db, other.StrategyInstanceId)
	if value, err := evaluator.Evaluate(ctx, created.StrategyID); err != nil || value.Allowed || value.Reason != domain.RiskReasonStopped {
		t.Fatal("browser fixture did not preserve independent stopped OKX diagnostics", err)
	}
	before := publicationInstance(ctx, t, db, source.StrategyInstanceId)
	projection := adminTradePortfolioRow(ctx, t, db, source.AccountId)
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
	runRiskEventBrowser(ctx, t, base, history, otherHistory[0].Id)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, source.AccountId)) ||
		!proto.Equal(other, riskEventSourceRow(ctx, t, db, other.StrategyInstanceId)) {
		t.Fatal("browser risk history query mutated strategy or producer state")
	}
	after := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(after) != len(history) {
		t.Fatal("browser created or removed risk history")
	}
	for i, row := range after {
		if !proto.Equal(row, history[i]) {
			t.Fatal("browser changed immutable historical fields")
		}
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, source.AccountId, "USDT", "799.8", "100.1")
	assertGatewayOrderCount(ctx, t, db, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 6)
}

func runRiskEventBrowser(ctx context.Context, t *testing.T, base string, history []*modelv1.RiskSnapshotEvent, otherID int64) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-risk-events-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-risk-events")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_RISK_PRIMARY="+strconv.FormatInt(history[3].Id, 10), "MUSKDEER_BROWSER_RISK_OTHER="+strconv.FormatInt(otherID, 10),
		"MUSKDEER_BROWSER_RISK_FIRST="+strconv.FormatInt(history[0].Id, 10), "MUSKDEER_BROWSER_RISK_NEXT="+strconv.FormatInt(history[1].Id, 10),
		"MUSKDEER_BROWSER_RISK_STRATEGY="+strconv.FormatInt(history[0].StrategyInstanceId, 10), "MUSKDEER_BROWSER_RISK_ACCOUNT="+history[0].AccountId,
		"MUSKDEER_BROWSER_RISK_INSTRUMENT="+strconv.FormatInt(history[0].InstrumentId, 10))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual risk history browser failed: %v\n%s", err, output)
	}
	t.Logf("actual risk history browser passed; screenshots=%s", artifacts)
}
