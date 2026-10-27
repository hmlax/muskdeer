//go:build integration

package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyCancellationActionWaitsForTerminalAndRetainsRacingFill(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.StrategyActionPlan{
		Key:                 "cancel-cycle-1",
		Kind:                domain.StrategyActionCancel,
		AccountID:           submit.Input.AccountID,
		InstrumentID:        submit.Input.InstrumentID,
		TargetIntentID:      live.ID,
		TargetClientOrderID: live.ClientOrderID,
	}
	planned, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := plan
	duplicate.Key = "cancel-cycle-other-key"
	if _, err := actions.Plan(ctx, submit.Input.Authority, duplicate); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("same unresolved target acquired a new cancel key")
	}
	attempted, err := actions.Attempt(ctx, planned, submit.Input.Authority, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	control := cancelInputFixture(ctx, submit, live.ClientOrderID, planned.IdempotencyKey)
	control.Deadline = attempted.Deadline
	pending, err := cancels.Cancel(ctx, control, submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatalf("original cancellation was not pending: %+v, %v", pending, err)
	}
	waiting, err := actions.Observe(ctx, attempted, submit.Input.Authority)
	if err != nil || waiting.State != domain.StrategyActionWaiting || waiting.CancellationID != pending.Request.ID {
		t.Fatalf("pending cancellation became a completed action: %+v, %v", waiting, err)
	}
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "journal-cancel-racing-fill",
		CounterpartySubject: "external-journal-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil {
		t.Fatal(err)
	}
	observed, err := actions.Observe(ctx, waiting, submit.Input.Authority)
	if err != nil || observed.State != domain.StrategyActionObserved || observed.ObservedOrderState != domain.OrderStateCanceled || observed.ObservedCancellationState != domain.CancellationConfirmed {
		t.Fatalf("terminal cancellation was not observed by its original key: %+v, %v", observed, err)
	}
	active, err := actions.Active(ctx, observed.StrategyID)
	if err != nil || len(active) != 0 {
		t.Fatal("terminal action remained active")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "11", "0")
	err = db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Where("id = ?", observed.ID).UpdateColumn("decision_key", "replacement-key").Error
	assertDatabaseCode(t, err, "23514")
}

func TestStrategyPendingActionCanBeDiscardedWithoutResurrectingItsKey(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := journalSubmitPlan(submit, "discard-1")
	planned, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	discarded, err := actions.Discard(ctx, planned, submit.Input.Authority)
	if err != nil || discarded.State != domain.StrategyActionDiscarded || !discarded.Deadline.IsZero() {
		t.Fatalf("unsent proposal was not safely discarded: %+v, %v", discarded, err)
	}
	retained, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil || retained.ID != planned.ID || retained.State != domain.StrategyActionDiscarded {
		t.Fatal("discarded planning identity was resurrected")
	}
	if _, err := actions.Attempt(ctx, discarded, submit.Input.Authority, time.Now().Add(time.Second)); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("discarded identity was dispatched")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestStrategyActionCommitFenceRollsBackStagedDispatch(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := actions.Plan(ctx, submit.Input.Authority, journalSubmitPlan(submit, "commit-fence"))
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:expire-strategy-action-authority"
	if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeFor[modelv1.StrategyAction]() {
			return
		}
		result := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.StrategyLease{}).
			Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).UpdateColumn("expires_at", 0)
		if result.Error != nil {
			tx.Error = errors.Join(tx.Error, result.Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	if _, err := actions.Attempt(ctx, planned, submit.Input.Authority, time.Now().Add(time.Second)); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("staged dispatch escaped final lease validation: %v", err)
	}
	retained, err := actions.ByKey(ctx, planned.StrategyID, planned.Plan.Key)
	if err != nil || retained.State != domain.StrategyActionPending || retained.Revision != planned.Revision || !retained.Deadline.IsZero() {
		t.Fatalf("failed fence left a dispatched action: %+v, %v", retained, err)
	}
}

func journalSubmitPlan(submit domain.GatewaySubmit, key string) domain.StrategyActionPlan {
	return domain.StrategyActionPlan{
		Key:          key,
		Kind:         domain.StrategyActionSubmit,
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		Side:         domain.SideBuy,
		Price:        decimal.NewFromInt(99),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
	}
}
