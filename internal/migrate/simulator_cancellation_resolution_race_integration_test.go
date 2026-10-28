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

func TestSimulatorCancellationResolutionWaitsForOriginalCoreTransaction(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	requests, err := repository.NewSimulatorCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	input := cancelInputFixture(ctx, submit, live.ClientOrderID, "in-flight-cancel-resolution")
	input.Deadline = time.Now().Add(time.Second)
	accepted, err := requests.Accept(ctx, input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := requests.Prepare(ctx, accepted)
	if err != nil {
		t.Fatal(err)
	}
	staged, release, resolverPID := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	var unblock sync.Once
	var tasks sync.WaitGroup
	const createHook = "muskdeer:test:uncommitted-cancel-resolution"
	const queryHook = "muskdeer:test:waiting-cancel-resolution"
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
	if err := db.Callback().Create().After("gorm:create").Register(createHook, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*modelv1.SimulatorCommand)
		if !ok || row.Action != domain.GatewayActionCancel {
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
		if !tx.DryRun && tx.Statement.Context.Value(simulatorResolutionProbeKey{}) == true {
			if _, ok := tx.Statement.Dest.(*modelv1.StrategyInstance); ok {
				sendBackendPID(tx, resolverPID)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	coreResult, resolved := make(chan error, 1), make(chan error, 1)
	coreInput := input
	coreInput.IdempotencyKey = prepared.CoreCommandKey
	tasks.Go(func() {
		_, err := core.Cancel(ctx, domain.GatewayCancel{
			IntentID:           live.ID,
			Input:              coreInput,
			ExecutionAuthority: submit.ExecutionAuthority,
		})
		coreResult <- err
	})
	select {
	case <-staged:
	case err := <-coreResult:
		t.Fatalf("core cancellation ended before controlled commit: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	timer := time.NewTimer(max(time.Until(input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	tasks.Go(func() {
		row, err := requests.Observe(context.WithValue(ctx, simulatorResolutionProbeKey{}, true), prepared, domain.CancellationUnknown, domain.IntentErrorOutcomeUnknown)
		if err == nil && (row.State != domain.CancellationRejected || row.ErrorCode != domain.IntentErrorCoreNotAccepted) {
			err = domain.ErrOutcomeUnknown
		}
		resolved <- err
	})
	waitBackendLock(ctx, t, db, receiveProbePID(ctx, t, resolverPID))
	unblock.Do(func() { close(release) })
	if err := <-coreResult; err == nil {
		t.Fatal("old cancellation committed after its final deadline fence")
	}
	if err := <-resolved; err != nil {
		t.Fatalf("resolution did not wait for original core rollback: %v", err)
	}
	parent, err := core.Find(ctx, input.AccountID, input.ClientOrderID)
	if err != nil || parent.State != domain.OrderStateLive || parent.CancelState != domain.CancelStateNone {
		t.Fatal("no-effect proof changed the live parent")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "799.8", "200.2")
}
