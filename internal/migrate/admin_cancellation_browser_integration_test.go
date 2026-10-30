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

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const cancellationBrowserReason = `撤单原因 <img src=x onerror="window.__cancelExecuted=true"> 保留原文`

func TestAdminCancellationBrowserConfirmsOneOrderAndRetainsOriginalKey(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "cancellation-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := riskControlFixture(t, db).Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(submit.Input.Authority.StrategyID)); err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	management := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	management.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcessFor(ctx, t, management, discovery, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, true, 90*time.Second)
	runCancellationBrowserPhase(ctx, t, base, before.Id, order.ID, 0, "request")
	var receipt modelv1.AdminOrderCancellation
	if err := db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Take(&receipt).Error; err != nil || receipt.OrderIntentId != order.ID || receipt.State != string(domain.AdminCancellationAccepted) || receipt.Reason != cancellationBrowserReason {
		t.Fatal("browser did not preserve the exact single-order request", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("browser request changed runtime state or authority before an executor existed")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	startNativeCancellationExecution(ctx, t, db, ca, postgres, naming, discovery, submit)
	waitNativeCancellationPending(ctx, t, db, order.ID)
	runCancellationBrowserPhase(ctx, t, base, before.Id, order.ID, receipt.Id, "pending")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "browser-cancellation-racing-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	client, jar := nativeRiskHTTPClient(t)
	nativeControlHTTPLogin(ctx, t, client, jar, base, "cancellation-viewer", domain.AdminRoleViewer)
	done := waitNativeCancellationComplete(ctx, t, client, base, &strategyv1.OrderCancellationReceipt{
		Id:         receipt.Id,
		StrategyId: before.Id,
	})
	runCancellationBrowserPhase(ctx, t, base, before.Id, order.ID, receipt.Id, "terminal")
	assertNativeCancellationFinal(ctx, t, db, core, submit, adminCancelRequest(order), done)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
}

func runCancellationBrowserPhase(ctx context.Context, t *testing.T, base string, strategyID, orderID, receiptID int64, phase string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-cancellation-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-cancellations")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_CANCEL_STRATEGY="+strconv.FormatInt(strategyID, 10), "MUSKDEER_BROWSER_CANCEL_ORDER="+strconv.FormatInt(orderID, 10),
		"MUSKDEER_BROWSER_CANCEL_RECEIPT="+strconv.FormatInt(receiptID, 10), "MUSKDEER_BROWSER_CANCEL_PHASE="+phase, "MUSKDEER_BROWSER_CANCEL_REASON="+cancellationBrowserReason)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual cancellation browser phase=%s failed: %v\n%s", phase, err, output)
	}
	t.Logf("actual cancellation browser phase=%s passed; screenshots=%s", phase, artifacts)
}
