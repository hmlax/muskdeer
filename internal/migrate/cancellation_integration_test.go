//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/simulator"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestOMSCancelPendingKeepsReservationThroughPartialFill(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	orders := intentServiceFixture(t, intents, core)
	live, err := orders.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	input := cancelInputFixture(ctx, submit, live.ClientOrderID, "cancel-pending-1")
	pending, err := cancels.Cancel(ctx, input, submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending || pending.Order.State != domain.OrderStateLive {
		t.Fatalf("OMS cancel acceptance = %+v, %v", pending, err)
	}
	assertReservedBudget(ctx, t, db, input.AccountID, "200")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "799.8", "200.2")
	if repeated, err := cancels.Cancel(ctx, input, submit.ExecutionAuthority); err != nil || repeated.Request.ID != pending.Request.ID || repeated.Request.State != domain.CancellationPending {
		t.Fatalf("repeated cancel was not idempotent: %+v, %v", repeated, err)
	}
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "cancel-race-fill-1",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := cancels.Recover(ctx, input.AccountID, pending.Request.ID)
	if err != nil || confirmed.Request.State != domain.CancellationConfirmed || confirmed.Order.State != domain.OrderStateCanceled || !confirmed.Order.FilledQuantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("OMS cancel/fill recovery = %+v, %v", confirmed, err)
	}
	if _, err := cancels.Recover(ctx, input.AccountID, pending.Request.ID); err != nil {
		t.Fatal(err)
	}
	assertReservedBudget(ctx, t, db, input.AccountID, "0")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "899.9", "0")
	assertCoreBalance(ctx, t, core, input.AccountID, "BTC", "11", "0")
}

func TestOMSLostCancelReplyRecoversPendingWithoutReleasingFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	orders := intentServiceFixture(t, intents, core)
	live, err := orders.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	lost, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit: simulator.ResponseNormal,
		Cancel: simulator.ResponseLost,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, lost)
	unknown, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "cancel-lost-1"), submit.ExecutionAuthority)
	if err != nil || unknown.Request.State != domain.CancellationUnknown {
		t.Fatalf("cancel loss lost its durable uncertainty: %+v, %v", unknown, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	pending, err := cancels.Recover(ctx, submit.Input.AccountID, unknown.Request.ID)
	if err != nil || pending.Request.State != domain.CancellationPending || pending.Order.CancelState != domain.CancelStatePending {
		t.Fatalf("lost cancellation did not query pending core status: %+v, %v", pending, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
}

func TestOMSCancelUnsentIntentIsAtomicAndNeverCallsCore(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	unsent, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	closed, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, unsent.ClientOrderID, "cancel-unsent-1"), submit.ExecutionAuthority)
	if err != nil || closed.Request.State != domain.CancellationConfirmed || closed.Order.State != domain.OrderStateCanceled || closed.Order.CoreOrderID != 0 {
		t.Fatalf("unsent cancellation = %+v, %v", closed, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	orders := intentServiceFixture(t, intents, core)
	if same, err := orders.Submit(ctx, submit.Input, submit.ExecutionAuthority); err != nil || same.State != domain.OrderStateCanceled {
		t.Fatalf("canceled unsent intent was dispatched: %+v, %v", same, err)
	}
}

func TestOMSCancelFullFillRaceKeepsFilledOrderAndFees(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "cancel-full-race"), submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "cancel-full-fill-1",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(2),
	}); err != nil {
		t.Fatal(err)
	}
	done, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID)
	if err != nil || done.Request.State != domain.CancellationConfirmed || done.Order.State != domain.OrderStateFilled {
		t.Fatalf("cancel reply erased a full fill: %+v, %v", done, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "12", "0")
}

func TestOMSCancelRequestKeyCannotTargetAnotherOrder(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	first, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	if _, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, first.ClientOrderID, "fixed-cancel-key"), submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	submit.Input.IdempotencyKey = "another-unsent-intent"
	second, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, second.ClientOrderID, "fixed-cancel-key"), submit.ExecutionAuthority); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("cancel key changed its original target: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
}

func TestOMSUnsentCancelOutboxFailureRetainsOriginalIntentAndBudget(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	unsent, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("controlled cancellation event write failure")
	const callbackName = "muskdeer:test:reject-cancellation-event"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*modelv1.OutboxEvent); ok && event.Event != nil && event.Event.Cancellation != nil {
			tx.Error = injected
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	cancels := cancellationServiceFixture(t, db, intents, core)
	if _, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, unsent.ClientOrderID, "failed-unsent-cancel"), submit.ExecutionAuthority); !errors.Is(err, injected) {
		t.Fatalf("cancellation event error lost its cause: %v", err)
	}
	retained, err := intents.ByID(ctx, submit.Input.AccountID, unsent.ID)
	if err != nil || retained.State != domain.OrderStateIntent || retained.Revision != unsent.Revision {
		t.Fatalf("failed event canceled original intent: %+v, %v", retained, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed cancellation left a command: %d, %v", count, err)
	}
}

func cancellationServiceFixture(t *testing.T, db *gorm.DB, intents domain.IntentExecutionRepository, gateway domain.TradingGateway) domain.CancellationService {
	t.Helper()
	requests, err := repository.NewCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := usecase.NewCancellationService(requests, intents, gateway)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func cancelInputFixture(ctx context.Context, submit domain.GatewaySubmit, clientID, key string) domain.CancelInput {
	deadline, _ := ctx.Deadline()
	return domain.CancelInput{
		IdempotencyKey: key,
		Authority:      submit.Input.Authority,
		AccountID:      submit.Input.AccountID,
		InstrumentID:   submit.Input.InstrumentID,
		ClientOrderID:  clientID,
		Deadline:       deadline,
	}
}
