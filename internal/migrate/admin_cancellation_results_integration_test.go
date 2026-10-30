//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminCancellationNotAcceptedRetainsReservationAndFinalFailure(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := repository.NewStrategyOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	work, err := worker.Prepare(ctx, submit.Input.Authority, 0)
	if err != nil || work.Action == nil {
		t.Fatal(err)
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, *work.Action, submit.Input.Authority, time.Now().Add(150*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	waitRiskDatabaseExpiry(ctx, t, db, actor.Principal.UserID, attempted.Deadline.UnixMilli())
	observed, err := actions.Observe(ctx, attempted, submit.Input.Authority)
	if err != nil || observed.State != domain.StrategyActionNotAccepted {
		t.Fatal("L1 deadline/absence proof did not seal original cancel key", err)
	}
	if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil {
		t.Fatal(err)
	}
	done, err := control.Receipt(ctx, actor.Token, receipt.StrategyID, receipt.ID)
	if err != nil || done.State != domain.AdminCancellationFailed || done.Outcome != domain.AdminCancellationNotAccepted || done.ObservedOrderState != domain.OrderStateIntent || done.CompletedAt.IsZero() {
		t.Fatal("sealed absent cancellation was treated as success or retried", err)
	}
	for range 2 {
		if next, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil || next.Action != nil || next.ReceiptID != 0 {
			t.Fatal("terminal failed request acquired a replacement action", err)
		}
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertAdminCancellationHistoryProtected(ctx, t, db, receipt.ID)
}

func TestAdminCancellationUnsentOrderCancelsWithoutCoreCommand(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
	if err != nil {
		t.Fatal(err)
	}
	// Existing cancellation atomically closes an intent that was never sent.
	cancels := cancellationServiceFixture(t, db, intents, core)
	if result, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, order.ClientOrderID, "unsent-order-explicit-cancel"), submit.ExecutionAuthority); err != nil || result.Request.State != domain.CancellationConfirmed {
		t.Fatal(err)
	}
	worker, err := repository.NewStrategyOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	if work, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil || work.Action != nil || work.ReceiptID != receipt.ID {
		t.Fatal("terminal unsent order invented an outgoing cancel", err)
	}
	done, err := control.Receipt(ctx, actor.Token, receipt.StrategyID, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertAdminCancellationTerminal(t, done, order.Revision)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorCommand{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertAdminCancellationHistoryProtected(ctx, t, db, receipt.ID)
}

func TestAdminCancellationDatabaseRejectsCoreOnlyCompletion(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("id = ?", receipt.ID).
		Select("State", "OutcomeCode", "Revision", "CompletedAt", "ObservedOrderRevision", "ObservedOrderState").Updates(&modelv1.AdminOrderCancellation{
		State:                 string(domain.AdminCancellationSucceeded),
		OutcomeCode:           domain.AdminCancellationTerminal,
		Revision:              2,
		CompletedAt:           proto.Int64(time.Now().UnixMilli()),
		ObservedOrderRevision: proto.Int64(order.Revision),
		ObservedOrderState:    proto.String(string(domain.OrderStateCanceled)),
	}).Error
	assertDatabaseCode(t, err, "23514")
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func assertAdminCancellationHistoryProtected(ctx context.Context, t *testing.T, db *gorm.DB, id int64) {
	t.Helper()
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("id = ?", id).UpdateColumn("reason", "rewritten history").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("id = ?", id).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("id = ?", id).Delete(&modelv1.AdminOrderCancellation{}).Error, "23514")
}
