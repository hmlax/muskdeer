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
	"github.com/hmlax/muskdeer/internal/simulator"
)

func TestAdminEmergencyScopeKeepsLostCancelAndRacingFillAcrossFencedTakeover(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	controller, execution, market := strategyControllerFixture(ctx, t, db, submit)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	submit = acquireStrategyForAdminCancel(ctx, t, leases, submit)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := execution.intents.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	progress := emergencyProgressStore(t, db)
	if enrolled, err := progress.Prepare(ctx, accepted.StrategyID, 0); err != nil || enrolled.Progress.Targets != 1 || enrolled.State != domain.AdminEmergencyCancelAccepted {
		t.Fatal("funded scope was not durably enrolled", err)
	}
	lost, err := simulator.WithFaults(execution.core, simulator.FaultPlan{
		Submit: simulator.ResponseNormal,
		Cancel: simulator.ResponseLost,
	})
	if err != nil {
		t.Fatal(err)
	}
	execution.requests = cancellationServiceFixture(t, db, execution.store, lost)
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: accepted.StrategyID,
	}
	expireLeaseFixture(ctx, t, db, key)
	for range 4 {
		strategyControllerTick(ctx, t, controller, market)
	}
	assertEmergencyStillPending(ctx, t, progress, accepted.StrategyID)
	if execution.cancels != 1 || execution.submits != 0 {
		t.Fatal("scope duplicated an unknown cancel or submitted replacement risk")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	if _, err := execution.core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "emergency-in-flight-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, key)
	other := strategyControllerWith(ctx, t, db, submit, leases, execution, market)
	for range 3 {
		strategyControllerTick(ctx, t, other, market)
	}
	assertEmergencyStillPending(ctx, t, progress, accepted.StrategyID)
	if _, err := execution.core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		strategyControllerTick(ctx, t, other, market)
		result, err := progress.Prepare(ctx, accepted.StrategyID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if result.State != domain.AdminEmergencyCancelSucceeded {
			continue
		}
		if result.Progress.Targets != 1 || result.Progress.Terminal != 1 || result.Progress.Failed != 0 || result.CompletedAt.IsZero() || execution.cancels != 1 || execution.submits != 0 {
			t.Fatal("scope completion lost terminal proof or original command identity")
		}
		final, err := execution.store.ByClient(ctx, submit.Input.AccountID, order.ClientOrderID)
		if err != nil || final.FilledQuantity.String() != "1" || !final.ReservedQuoteNotional.IsZero() {
			t.Fatal("scope completion lost fill or released unconfirmed reservations", err)
		}
		assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "899.9", "0")
		assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
		assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
		assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
		assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
		assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
		return
	}
	t.Fatal("scope did not persist completion after authoritative terminal confirmation")
}

func TestAdminEmergencyProgressWaitsForUnsentSubmissionBarrier(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	action, err := actions.Plan(ctx, submit.Input.Authority, domain.StrategyActionPlan{
		Key:          "emergency-pending-submit-action",
		Kind:         domain.StrategyActionSubmit,
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		Side:         domain.SideBuy,
		Price:        decimal.NewFromInt(99),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	work := emergencyProgressStore(t, db)
	result, err := work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || result.State != domain.AdminEmergencyCancelAccepted || result.Progress.PendingSubmissions != 1 || result.Progress.EnrollmentComplete {
		t.Fatal("unobserved outgoing submission was hidden by zero OMS targets", err)
	}
	var task modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.ID).Take(&task).Error; err != nil || task.EnrollmentUpperId != nil {
		t.Fatal("cutoff was sealed before the original submit could be ruled out", err)
	}
	if _, err := actions.Discard(ctx, action, submit.Input.Authority); err != nil {
		t.Fatal(err)
	}
	result, err = work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || result.State != domain.AdminEmergencyCancelSucceeded || result.Progress.PendingSubmissions != 0 {
		t.Fatal("proven never-sent action kept a genuinely empty scope unresolved", err)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminEmergencyEnrollmentFailureRollsBackCursorChildrenAndTheirAudits(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	var before modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.ID).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("test-only enrollment storage failure")
	const callback = "test:fail-emergency-child"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, child := tx.Statement.Dest.(*modelv1.AdminOrderCancellation); child {
			tx.Error = injected
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	result, err := emergencyProgressStore(t, db).Prepare(ctx, accepted.StrategyID, 0)
	if !errors.Is(err, injected) || result.AcceptanceID != 0 {
		t.Fatal("failed enrollment returned partial progress or lost its private cause", err)
	}
	var after modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ?", before.Id).Take(&after).Error; err != nil || !proto.Equal(&before, &after) {
		t.Fatal("failed child write advanced durable cursor or sealed cutoff", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertEmergencyStillPending(ctx context.Context, t *testing.T, work domain.StrategyEmergencyCancellations, strategyID int64) {
	t.Helper()
	result, err := work.Prepare(ctx, strategyID, 0)
	if err != nil || result.State != domain.AdminEmergencyCancelAccepted || result.Progress.Terminal != 0 || !result.CompletedAt.IsZero() {
		t.Fatal("unconfirmed cancellation was treated as final scope proof", err)
	}
}
