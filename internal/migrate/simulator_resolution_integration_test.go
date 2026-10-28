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
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestSimulatorMissingSubmissionResolvesOnlyAfterOriginalDeadline(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, generic := executionFixture(t, db)
	store, err := repository.NewSimulatorIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(ctx, accepted, submit.Input.Authority, submit.ExecutionAuthority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	orders := intentServiceFixture(t, store, core)
	before, err := orders.Recover(ctx, submit.Input.AccountID, prepared.ID)
	if err != nil || before.State != domain.OrderStateUnknown {
		t.Fatalf("unexpired missing core request was resolved: %+v, %v", before, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	time.Sleep(max(time.Until(prepared.Input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	// The generic/external contract retains uncertainty even after the local
	// deadline. Only the explicitly selected shared-database venue may prove it.
	external, err := intentServiceFixture(t, generic, core).Recover(ctx, submit.Input.AccountID, prepared.ID)
	if err != nil || external.State != domain.OrderStateUnknown {
		t.Fatal("generic missing-core read released uncertainty")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	resolved, err := orders.Recover(ctx, submit.Input.AccountID, prepared.ID)
	if err != nil || resolved.State != domain.OrderStateRejected || resolved.ErrorCode != domain.IntentErrorCoreNotAccepted || resolved.CoreOrderID != 0 || !resolved.ReservedQuoteNotional.IsZero() {
		t.Fatalf("expired serialized L1 request did not resolve: %+v, %v", resolved, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	// Even a privileged direct command with no intent pointer cannot renew the
	// sealed client identity. Its staged fund changes must roll back as well.
	input := prepared.Input
	input.Deadline = time.Now().Add(time.Second)
	input.IdempotencyKey = domain.CoreSubmitKeyPrefix + prepared.ClientOrderID
	_, err = core.Submit(ctx, domain.GatewaySubmit{
		Input:              input,
		ClientOrderID:      prepared.ClientOrderID,
		ExecutionAuthority: prepared.ExecutionAuthority,
	})
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", resolved.ID).Delete(&modelv1.OrderIntent{}).Error
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", resolved.ID).UpdateColumn("error_code", domain.IntentErrorOutcomeUnknown).Error
	assertDatabaseCode(t, err, "23514")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}

func TestSimulatorAcceptedButUnpreparedIntentClosesOnlyItsExpiredIdentity(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, generic := executionFixture(t, db)
	store, err := repository.NewSimulatorIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	submit.Input.Deadline = time.Now().Add(time.Second)
	accepted, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	orders := intentServiceFixture(t, store, core)
	before, err := orders.Recover(ctx, submit.Input.AccountID, accepted.ID)
	if err != nil || before.State != domain.OrderStateIntent {
		t.Fatal("unexpired accepted intent changed without preparation")
	}
	time.Sleep(max(time.Until(accepted.Input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	external, err := intentServiceFixture(t, generic, core).Recover(ctx, submit.Input.AccountID, accepted.ID)
	if err != nil || external.State != domain.OrderStateIntent {
		t.Fatal("generic repository assumed local core authority")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	resolved, err := orders.Recover(ctx, submit.Input.AccountID, accepted.ID)
	if err != nil || resolved.State != domain.OrderStateRejected || resolved.ErrorCode != domain.IntentErrorCoreNotAccepted {
		t.Fatalf("unprepared expired intent blocked recovery: %+v, %v", resolved, err)
	}
	if _, err := store.Prepare(ctx, resolved, submit.Input.Authority, submit.ExecutionAuthority, time.Now().Add(time.Second)); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("resolved original intent became dispatchable")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestSimulatorResolutionRejectsForgedEarlyAndSnapshotProofAndKeepsRealCore(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, _ := executionFixture(t, db)
	store, err := repository.NewSimulatorIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	live, err := intentServiceFixture(t, store, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatal("real core fixture did not submit")
	}
	unknown, err := store.Unknown(ctx, live, domain.IntentErrorCoreNotFound)
	if err != nil || unknown.State != domain.OrderStateUnknown || !unknown.ReservedQuoteNotional.Equal(decimal.NewFromInt(200)) {
		t.Fatal("missing read erased real core evidence")
	}
	forge := func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", live.ID).
			Select("State", "ErrorCode", "CoreOrderId", "CoreRevision", "ReservedQuoteNotional").Updates(&modelv1.OrderIntent{
			State:                 string(domain.OrderStateRejected),
			ErrorCode:             domain.IntentErrorCoreNotAccepted,
			ReservedQuoteNotional: "0",
		}).Error
	}
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(forge)
	assertDatabaseCode(t, err, "23514")
	time.Sleep(max(time.Until(live.Input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(forge, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Transaction(forge)
	assertDatabaseCode(t, err, "23514")
	actual, err := intentServiceFixture(t, store, core).Recover(ctx, submit.Input.AccountID, live.ID)
	if err != nil || actual.State != domain.OrderStateLive || actual.CoreOrderID != live.CoreOrderID {
		t.Fatal("committed original core order was not recovered")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 1)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
}

func TestSimulatorResolutionRetainsCancellationUntilParentAuthorityIsKnown(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, _ := executionFixture(t, db)
	store, err := repository.NewSimulatorIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(ctx, accepted, submit.Input.Authority, submit.ExecutionAuthority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	requests, err := repository.NewCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	cancels, err := usecase.NewCancellationService(requests, store, core)
	if err != nil {
		t.Fatal(err)
	}
	request, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, prepared.ClientOrderID, "missing-parent-cancel"), submit.ExecutionAuthority)
	if err != nil || request.Request.State == domain.CancellationConfirmed {
		t.Fatal("unexpired missing parent was treated as canceled")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	time.Sleep(max(time.Until(prepared.Input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	result, err := cancels.RecoverByKey(ctx, submit.Input.AccountID, request.Request.Input.IdempotencyKey)
	if err != nil || result.Request.State != domain.CancellationConfirmed || result.Order.State != domain.OrderStateRejected || result.Order.ErrorCode != domain.IntentErrorCoreNotAccepted {
		t.Fatalf("definitive missing parent did not complete cancellation: %+v, %v", result, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	if _, err := store.Reject(ctx, result.Order, domain.IntentErrorCoreNotAccepted); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("ordinary rejection bypassed authoritative no-effect proof")
	}
}
