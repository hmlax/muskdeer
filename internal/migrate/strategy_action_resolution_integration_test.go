//go:build integration

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyActionAbsenceRequiresExpiredDeadlineAndNeverRevivesOriginalKey(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := journalSubmitPlan(submit, "deadline-proven-absent")
	planned, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, planned, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := actions.Observe(ctx, attempted, submit.Input.Authority)
	if err != nil || unknown.State != domain.StrategyActionUnknown {
		t.Fatal("absence before the original deadline was treated as no effect")
	}
	time.Sleep(1100 * time.Millisecond)
	resolved, err := actions.Observe(ctx, unknown, submit.Input.Authority)
	if err != nil || resolved.State != domain.StrategyActionNotAccepted || resolved.IntentID != 0 || resolved.ObservedOrderState != domain.OrderStateUnspecified {
		t.Fatalf("serialized expired request without OMS/core acceptance was not resolved: %+v, %v", resolved, err)
	}
	if _, err := actions.Attempt(ctx, resolved, submit.Input.Authority, time.Now().Add(time.Second)); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("resolved original action became eligible for redispatch")
	}
	again, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil || again.ID != resolved.ID || again.State != domain.StrategyActionNotAccepted {
		t.Fatal("original decision key was resurrected")
	}
	active, err := actions.Active(ctx, submit.Input.Authority.StrategyID)
	if err != nil || len(active) != 0 {
		t.Fatal("proven unaccepted action still blocked lifecycle completion")
	}
	feed, publisher, snapshots, previous := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := snapshots.Publish(ctx, publisher, previous.Revision, marketReadingFixture(feed, previous.Reading.Sequence+1, "100")); err != nil {
		t.Fatal(err)
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	if decision, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID); err != nil || !decision.Allowed {
		t.Fatalf("healthy control for original-key rejection was unavailable: %+v, %v", decision, err)
	}
	input := submit.Input
	input.IdempotencyKey = resolved.IdempotencyKey
	input.Price, input.Quantity = plan.Price, plan.Quantity
	input.TimeInForce = plan.TimeInForce
	input.Deadline = time.Now().Add(time.Second)
	if _, err := intents.Accept(ctx, input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("renewing the original key deadline bypassed its durable resolution: %v", err)
	}
	core, _ := executionFixture(t, db)
	input.IdempotencyKey = domain.CoreSubmitKeyPrefix + resolved.ClientOrderID
	if _, err := core.Submit(ctx, domain.GatewaySubmit{
		Input:              input,
		ClientOrderID:      resolved.ClientOrderID,
		ExecutionAuthority: submit.ExecutionAuthority,
	}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("renewed direct L1 command bypassed original-key resolution: %v", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorCommand{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
}

func TestStrategyResolutionDatabaseRejectsEarlyOrStaleSnapshotProof(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := actions.Plan(ctx, submit.Input.Authority, journalSubmitPlan(submit, "forged-absence-proof"))
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, planned, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	forge := func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Where("id = ?", attempted.ID).
			Select("State", "Revision").Updates(&modelv1.StrategyAction{
			State:    string(domain.StrategyActionNotAccepted),
			Revision: attempted.Revision + 1,
		}).Error
	}
	err = db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Transaction(forge)
	assertDatabaseCode(t, err, "23514")
	time.Sleep(1100 * time.Millisecond)
	err = db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Transaction(forge, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	assertDatabaseCode(t, err, "23514")
	current, err := actions.ByKey(ctx, attempted.StrategyID, attempted.Plan.Key)
	if err != nil || current.State != domain.StrategyActionDispatching || current.Revision != attempted.Revision {
		t.Fatal("rejected proof changed the durable attempt")
	}
	resolved, err := actions.Observe(ctx, current, submit.Input.Authority)
	if err != nil || resolved.State != domain.StrategyActionNotAccepted {
		t.Fatalf("current serialized observation did not resolve absence: %+v, %v", resolved, err)
	}
}

func TestUnacceptedCancellationKeepsFundsAndSealsOldCoreCommand(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Price, submit.Input.Quantity = decimal.NewFromInt(99), decimal.NewFromInt(1)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatalf("cancellation resolution fixture: %+v, %v", live, err)
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.StrategyActionPlan{
		Key:                 "expired-cancel-never-accepted",
		Kind:                domain.StrategyActionCancel,
		AccountID:           submit.Input.AccountID,
		InstrumentID:        submit.Input.InstrumentID,
		TargetIntentID:      live.ID,
		TargetClientOrderID: live.ClientOrderID,
	}
	pending, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, pending, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	resolved, err := actions.Observe(ctx, attempted, submit.Input.Authority)
	if err != nil || resolved.State != domain.StrategyActionNotAccepted || resolved.CancellationID != 0 || resolved.IntentID != live.ID {
		t.Fatalf("absent expired cancellation did not retain its separate parent: %+v, %v", resolved, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "99")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "900.901", "99.099")
	control := cancelInputFixture(ctx, submit, live.ClientOrderID, resolved.IdempotencyKey)
	control.Deadline = time.Now().Add(time.Second)
	requests, err := repository.NewCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Accept(ctx, control, submit.ExecutionAuthority); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("old cancellation key revived: %v", err)
	}
	digest, err := domain.IntentClientOrderID(control.AccountID, control.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	control.IdempotencyKey = domain.CoreCancelKeyPrefix + digest
	if _, err := core.Cancel(ctx, domain.GatewayCancel{
		IntentID:           live.ID,
		Input:              control,
		ExecutionAuthority: submit.ExecutionAuthority,
	}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("late old core cancellation was not sealed: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "99")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "900.901", "99.099")
	plan.Key = "fresh-control-after-proven-absence"
	fresh, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = actions.Attempt(ctx, fresh, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	control = cancelInputFixture(ctx, submit, live.ClientOrderID, fresh.IdempotencyKey)
	control.Deadline = fresh.Deadline
	if _, err := cancels.Cancel(ctx, control, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, control.AccountID, control.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := cancels.RecoverByKey(ctx, control.AccountID, control.IdempotencyKey)
	if err != nil || confirmed.Request.State != domain.CancellationConfirmed {
		t.Fatal("fresh control did not confirm the real cancellation")
	}
	if _, err := actions.Observe(ctx, fresh, submit.Input.Authority); err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ?", confirmed.Request.ID).Delete(&modelv1.CancellationRequest{}).Error
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ?", confirmed.Request.ID).UpdateColumn("delete_at", time.Now().UnixMilli()).Error
	assertDatabaseCode(t, err, "23514")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}
