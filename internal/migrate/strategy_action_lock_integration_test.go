//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

type actionAdmissionProbeKey struct{}

func TestActionCoreFenceDoesNotUpgradeBehindConcurrentAdmission(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := journalSubmitPlan(submit, "serialized-core-and-admission")
	pending, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, pending, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	input := submit.Input
	input.IdempotencyKey, input.Price, input.Quantity, input.TimeInForce = attempted.IdempotencyKey, plan.Price, plan.Quantity, plan.TimeInForce
	input.Deadline = attempted.Deadline
	accepted, err := intents.Accept(ctx, input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := intents.Prepare(ctx, accepted, input.Authority, submit.ExecutionAuthority, input.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	staged, release, admissionPID := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	var unblock sync.Once
	var tasks sync.WaitGroup
	const createHook = "muskdeer:test:hold-core-before-action-fence"
	const queryHook = "muskdeer:test:concurrent-action-admission"
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		cancel()
		tasks.Wait()
		if err := db.Callback().Create().Remove(createHook); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(queryHook); err != nil {
			t.Error(err)
		}
	})
	if err := db.Callback().Create().Before("gorm:create").Register(createHook, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorCommand); !ok {
			return
		}
		close(staged)
		select {
		case <-release:
		case <-ctx.Done():
			tx.Error = errors.Join(tx.Error, ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
		if !tx.DryRun && tx.Statement.Context.Value(actionAdmissionProbeKey{}) == true {
			if _, ok := tx.Statement.Dest.(*modelv1.StrategyInstance); ok {
				sendBackendPID(tx, admissionPID)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	coreResult, admissionResult := make(chan globalCoreResult, 1), make(chan error, 1)
	coreInput := prepared.Input
	coreInput.IdempotencyKey = domain.CoreSubmitKeyPrefix + prepared.ClientOrderID
	tasks.Go(func() {
		order, err := core.Submit(ctx, domain.GatewaySubmit{
			IntentID:           prepared.ID,
			Input:              coreInput,
			ClientOrderID:      prepared.ClientOrderID,
			ExecutionAuthority: prepared.ExecutionAuthority,
		})
		coreResult <- globalCoreResult{
			order: order,
			err:   err,
		}
	})
	select {
	case <-staged:
	case result := <-coreResult:
		t.Fatalf("core ended before controlled boundary: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	other := input
	other.IdempotencyKey = "second-admission-same-strategy"
	tasks.Go(func() {
		_, err := intents.Accept(context.WithValue(ctx, actionAdmissionProbeKey{}, true), other, submit.ExecutionAuthority)
		admissionResult <- err
	})
	waitBackendLock(ctx, t, db, receiveProbePID(ctx, t, admissionPID))
	unblock.Do(func() { close(release) })
	result := <-coreResult
	if result.err != nil {
		t.Fatalf("action core fence deadlocked with concurrent admission: %v; cause: %v", result.err, errors.Unwrap(result.err))
	}
	if err := <-admissionResult; err != nil {
		t.Fatalf("concurrent admission failed: %v; cause: %v", err, errors.Unwrap(err))
	}
	if _, err := intents.Confirm(ctx, prepared, result.order); err != nil {
		t.Fatal(err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, input.AccountID, "198")
}
