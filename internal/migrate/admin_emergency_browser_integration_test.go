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

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const emergencyBrowserReason = `范围撤单 <img src=x onerror="window.__emergencyExecuted=true"> 两个实际挂单`

func TestAdminEmergencyBrowserConfirmsScopeAndKeepsDurableResult(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "emergency-viewer", domain.AdminRoleViewer, 231, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	service := intentServiceFixture(t, intents, core)
	submit.Input.Deadline = time.Now().Add(time.Second)
	first, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	input := submit.Input
	input.IdempotencyKey = "emergency-browser-second-real-order"
	input.Quantity = decimal.RequireFromString("0.5")
	input.Deadline = time.Now().Add(time.Second)
	second, err := service.Submit(ctx, input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	management := businessProcessConfig(t, ca, postgres, nil, discovery.Config, "strategy")
	management.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcessFor(ctx, t, management, discovery.Client, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, discovery.Config, true, 90*time.Second)
	runEmergencyBrowserPhase(ctx, t, base, before.Id, 0, "request")
	var accepted modelv1.AdminEmergencyCancellation
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Take(&accepted).Error; err != nil || accepted.StrategyInstanceId != before.Id || accepted.Reason != emergencyBrowserReason || accepted.ExpectedRiskRevision != 1 || accepted.RiskRevision != 2 {
		t.Fatal("browser did not persist the confirmed strategy/risk/reason", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("browser acceptance changed desired/runtime state before an owner existed")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "250")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "749.75", "250.25")
	startNativeCancellationExecution(ctx, t, db, ca, postgres, discovery.Config, discovery.Client, submit)
	waitNativeCancellationPending(ctx, t, db, first.ID)
	waitNativeCancellationPending(ctx, t, db, second.ID)
	runEmergencyBrowserPhase(ctx, t, base, before.Id, accepted.Id, "pending")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "250")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "749.75", "250.25")
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       first.ClientOrderID,
		TradeID:             "browser-emergency-racing-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	for _, order := range []domain.IntentSnapshot{first, second} {
		if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	proof, cancelProof := context.WithTimeout(ctx, 5*time.Second)
	waitEmergencyProcessTask(proof, t, db, accepted.Id, domain.AdminEmergencyCancelSucceeded)
	cancelProof()
	runEmergencyBrowserPhase(ctx, t, base, before.Id, accepted.Id, "terminal")
	for index, original := range []domain.IntentSnapshot{first, second} {
		order, err := intents.ByID(ctx, submit.Input.AccountID, original.ID)
		filled := decimal.Zero
		if index == 0 {
			filled = decimal.NewFromInt(1)
		}
		if err != nil || order.State != domain.OrderStateCanceled || !order.ReservedQuoteNotional.IsZero() || !order.FilledQuantity.Equal(filled) {
			t.Fatal("browser result lost terminal fact, reservation or racing fill", err)
		}
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
	if activationBudget(ctx, t, db, before.Id).Enabled {
		t.Fatal("scope completion restored risk without administrator authorization")
	}
	for _, entity := range []any{&modelv1.OrderIntent{}, &modelv1.SimulatorOrder{}, &modelv1.AdminOrderCancellation{}, &modelv1.StrategyAction{}, &modelv1.CancellationRequest{}} {
		assertPortfolioRowCount(ctx, t, db, entity, 2)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
}

func runEmergencyBrowserPhase(ctx context.Context, t *testing.T, base string, strategyID, receiptID int64, phase string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-emergency-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-emergencies")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_EMERGENCY_STRATEGY="+strconv.FormatInt(strategyID, 10), "MUSKDEER_BROWSER_EMERGENCY_RECEIPT="+strconv.FormatInt(receiptID, 10),
		"MUSKDEER_BROWSER_EMERGENCY_PHASE="+phase, "MUSKDEER_BROWSER_EMERGENCY_REASON="+emergencyBrowserReason)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual emergency browser phase=%s failed: %v\n%s", phase, err, output)
	}
	t.Logf("actual emergency browser phase=%s passed; screenshots=%s", phase, artifacts)
}
