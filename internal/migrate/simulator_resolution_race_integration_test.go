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

type simulatorResolutionProbeKey struct{}

func TestSimulatorResolutionWaitsForUncommittedCoreAndItsFinalDeadlineFence(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
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
	staged, release, resolverPID := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	var unblock sync.Once
	var tasks sync.WaitGroup
	const createHook = "muskdeer:test:uncommitted-core-resolution"
	const queryHook = "muskdeer:test:waiting-core-resolution"
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
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
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
	input := prepared.Input
	input.IdempotencyKey = domain.CoreSubmitKeyPrefix + prepared.ClientOrderID
	tasks.Go(func() {
		_, err := core.Submit(ctx, domain.GatewaySubmit{
			IntentID:           prepared.ID,
			Input:              input,
			ClientOrderID:      prepared.ClientOrderID,
			ExecutionAuthority: prepared.ExecutionAuthority,
		})
		coreResult <- err
	})
	select {
	case <-staged:
	case err := <-coreResult:
		t.Fatalf("core failed before controlled commit: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	tasks.Go(func() {
		row, err := store.Unknown(context.WithValue(ctx, simulatorResolutionProbeKey{}, true), prepared, domain.IntentErrorCoreNotFound)
		if err == nil && (row.State != domain.OrderStateRejected || row.ErrorCode != domain.IntentErrorCoreNotAccepted) {
			err = domain.ErrOutcomeUnknown
		}
		resolved <- err
	})
	waitBackendLock(ctx, t, db, receiveProbePID(ctx, t, resolverPID))
	select {
	case err := <-resolved:
		t.Fatalf("resolution bypassed uncommitted core: %v", err)
	default:
	}
	timer := time.NewTimer(max(time.Until(prepared.Input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	unblock.Do(func() { close(release) })
	if err := <-coreResult; err == nil {
		t.Fatal("staged core effect committed after its final deadline fence")
	}
	if err := <-resolved; err != nil {
		t.Fatalf("resolution did not wait for final core rollback: %v", err)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorCommand{}, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}
