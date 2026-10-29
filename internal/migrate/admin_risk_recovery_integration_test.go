//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskCancelConfirmationIsRequiredBeforeFreshAdminRecovery(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	block := riskBlockRequest(before.Id)
	blocked, err := control.Change(ctx, operator.Token, operator.CSRF, block)
	if err != nil || blocked.Revision != 2 {
		t.Fatal("funded risk block failed", err)
	}
	restore := riskRestoreRequest(before)
	if _, err := control.Change(ctx, operator.Token, operator.CSRF, restore); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("operator restored risk", err)
	}
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, restore); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("live funded obligations permitted recovery", err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "risk-blocked-cancel-0001"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal("blocked strategy could not request a fenced cancellation", err)
	}
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, restore); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("unconfirmed cancellation permitted recovery", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID)
	if err != nil || confirmed.Request.State != domain.CancellationConfirmed || confirmed.Order.State != domain.OrderStateCanceled {
		t.Fatal("core confirmation did not release the obligation", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertRiskControlState(ctx, t, control, operator, before.Id, false, 2)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	restored, err := control.Change(ctx, admin.Token, admin.CSRF, restore)
	if err != nil || !restored.Enabled || restored.PreviousEnabled || restored.Revision != 3 {
		t.Fatal("clear obligations and fresh checks did not permit admin recovery", err)
	}
	assertRiskControlState(ctx, t, control, admin, before.Id, true, 3)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("risk control implicitly changed runtime/configuration/ownership")
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", before.Id).Take(&risk).Error; err != nil || risk.AllowNewRisk {
		t.Fatal("recovery issued a cached trading grant", err)
	}
	replay, err := control.Change(ctx, operator.Token, operator.CSRF, block)
	if err != nil || replay.ID != blocked.ID || replay.Enabled || replay.Revision != 2 {
		t.Fatal("historical block replay changed the later risk setting", err)
	}
	assertRiskControlState(ctx, t, control, operator, before.Id, true, 3)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	assertGatewayOrderCount(ctx, t, db, 1)
}

func TestAdminRiskPendingActionWithNoReservationStillDeniesRecovery(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	action, err := actions.Plan(ctx, submit.Input.Authority, domain.StrategyActionPlan{
		Key:          "risk-pending-submit-action",
		Kind:         domain.StrategyActionSubmit,
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		Side:         domain.SideBuy,
		Price:        decimal.NewFromInt(99),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
	})
	if err != nil || action.State != domain.StrategyActionPending {
		t.Fatal(err)
	}
	control := riskControlFixture(t, db)
	instance := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	if _, err := control.Change(ctx, operator.Token, operator.CSRF, riskBlockRequest(instance.Id)); err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(instance)); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("zero reservation hid an unsubmitted pending command", err)
	}
	assertRiskControlState(ctx, t, control, admin, instance.Id, false, 2)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}
