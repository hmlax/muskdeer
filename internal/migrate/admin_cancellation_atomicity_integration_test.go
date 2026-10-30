//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminCancellationConcurrentReplayAndExistingActionHaveOneEffect(t *testing.T) {
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
	request := adminCancelRequest(order)
	type outcome struct {
		receipt domain.AdminCancellationReceipt
		err     error
	}
	// Four bounded requests each publish one immutable value before joining.
	results := make(chan outcome, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			receipt, err := control.Request(ctx, actor.Token, actor.CSRF, request)
			results <- outcome{
				receipt: receipt,
				err:     err,
			}
		})
	}
	workers.Wait()
	var original domain.AdminCancellationReceipt
	for range 4 {
		result := <-results
		if result.err != nil || result.receipt.ID <= 0 {
			t.Fatal("concurrent same-key cancellation failed", result.err)
		}
		if original.ID == 0 {
			original = result.receipt
		}
		if original != result.receipt {
			t.Fatal("concurrent replay created multiple management effects")
		}
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := actions.Plan(ctx, submit.Input.Authority, domain.StrategyActionPlan{
		Key:                 "existing-protection-cancel",
		Kind:                domain.StrategyActionCancel,
		AccountID:           submit.Input.AccountID,
		InstrumentID:        submit.Input.InstrumentID,
		TargetIntentID:      order.ID,
		TargetClientOrderID: order.ClientOrderID,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := repository.NewStrategyOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	work, err := worker.Prepare(ctx, submit.Input.Authority, 0)
	if err != nil || work.Action == nil || work.Action.ID != existing.ID || work.Action.IdempotencyKey != existing.IdempotencyKey {
		t.Fatal("management did not adopt original unresolved cancellation", err)
	}
	request.Key = "another-explicit-cancel-key"
	second, err := control.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	work, err = worker.Prepare(ctx, submit.Input.Authority, original.ID)
	if err != nil || work.ReceiptID != second.ID || work.Action == nil || work.Action.ID != existing.ID {
		t.Fatal("later request created a competing cancellation action", err)
	}
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: request.StrategyID,
	})
	if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("expired owner prepared management work", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func TestAdminCancellationWriteFailuresRollBackAuditAndRequest(t *testing.T) {
	for _, failReceipt := range []bool{false, true} {
		name := "audit"
		if failReceipt {
			name = "receipt"
		}
		t.Run(name, func(t *testing.T) {
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
			before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
			injected := errors.New("test-only cancellation storage failure")
			const callback = "test:reject-admin-cancellation-write"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				_, audit := tx.Statement.Dest.(*modelv1.AdminAudit)
				_, receipt := tx.Statement.Dest.(*modelv1.AdminOrderCancellation)
				if failReceipt && receipt || !failReceipt && audit {
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
			control, err := repository.NewAdminOrderCancellations(db)
			if err != nil {
				t.Fatal(err)
			}
			result, err := control.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
			if !errors.Is(err, domain.ErrAdminControlUnavailable) || !errors.Is(err, injected) || result.ID != 0 {
				t.Fatal("failed cancellation storage returned partial success or lost private cause", err)
			}
			if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
				t.Fatal("failed request changed strategy identity or runtime target")
			}
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
			assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
		})
	}
}

func TestAdminCancellationSessionCannotExpireBetweenReceiptAndCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	short := riskActorFixture(ctx, t, db, "cancel-expiring-operator", domain.AdminRoleOperator, 61, time.Second)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:expire-cancellation-authority-after-receipt"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminOrderCancellation); ok && tx.Error == nil {
			waitRiskDatabaseExpiry(ctx, t, db, actor.Principal.UserID, short.Principal.ExpiresAt.UnixMilli())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	result, err := control.Request(ctx, short.Token, short.CSRF, adminCancelRequest(order))
	if !errors.Is(err, domain.ErrUnauthenticated) || result.ID != 0 {
		t.Fatal("expired authority committed management cancellation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}
