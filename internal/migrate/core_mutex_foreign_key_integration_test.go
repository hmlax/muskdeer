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

func TestCoreMutexDoesNotBlockOtherStrategyActionForeignKeys(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	sibling := admissionSibling(ctx, t, db, submit)
	core, _ := executionFixture(t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	staged, release, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var unblock sync.Once
	var tasks sync.WaitGroup
	const hook = "muskdeer:test:core-mutex-before-commit"
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		cancel()
		tasks.Wait()
		if err := db.Callback().Create().Remove(hook); err != nil {
			t.Error(err)
		}
	})
	if err := db.Callback().Create().After("gorm:create").Register(hook, func(tx *gorm.DB) {
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
	tasks.Go(func() {
		_, err := core.Submit(ctx, submit)
		result <- err
	})
	select {
	case <-staged:
	case err := <-result:
		t.Fatalf("core did not reach the controlled commit boundary: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Merely recording another strategy's PENDING action is not a matching
	// effect. Its instrument FK must remain compatible with the core mutex.
	planCtx, stop := context.WithTimeout(ctx, 250*time.Millisecond)
	planned, planErr := actions.Plan(planCtx, sibling.Input.Authority, journalSubmitPlan(sibling, "independent-pending-action"))
	stop()
	unblock.Do(func() { close(release) })
	if planErr != nil || planned.State != domain.StrategyActionPending {
		t.Fatalf("core mutex blocked an independent action foreign key: %v", planErr)
	}
	if err := <-result; err != nil {
		t.Fatalf("core transaction failed after metadata progress: %v", err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}
