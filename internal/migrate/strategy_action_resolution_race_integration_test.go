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

func TestActionResolutionWaitsForInFlightAdmissionPastItsDeadline(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := journalSubmitPlan(submit, "in-flight-admission-must-be-observed")
	pending, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil {
		t.Fatal(err)
	}
	attempted, err := actions.Attempt(ctx, pending, submit.Input.Authority, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	staged, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	var tasks sync.WaitGroup
	defer func() {
		unblock.Do(func() { close(release) })
		cancel()
		tasks.Wait()
	}()
	const callback = "muskdeer:test:hold-action-admission-commit"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*modelv1.OrderIntent)
		if !ok || row.IdempotencyKey != attempted.IdempotencyKey {
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
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	type acceptance struct {
		row domain.IntentSnapshot
		err error
	}
	type observation struct {
		row domain.StrategyAction
		err error
	}
	accepted, observed := make(chan acceptance, 1), make(chan observation, 1)
	input := submit.Input
	input.IdempotencyKey, input.Price, input.Quantity, input.TimeInForce = attempted.IdempotencyKey, plan.Price, plan.Quantity, plan.TimeInForce
	input.Deadline = attempted.Deadline
	tasks.Go(func() {
		row, err := intents.Accept(ctx, input, submit.ExecutionAuthority)
		accepted <- acceptance{
			row: row,
			err: err,
		}
	})
	select {
	case <-staged:
	case result := <-accepted:
		t.Fatalf("original admission ended before its controlled commit: %+v, %v", result.row, result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	tasks.Go(func() {
		row, err := actions.Observe(ctx, attempted, submit.Input.Authority)
		observed <- observation{
			row: row,
			err: err,
		}
	})
	select {
	case result := <-observed:
		t.Fatalf("resolution bypassed in-flight strategy admission: %+v, %v", result.row, result.err)
	case <-time.After(75 * time.Millisecond):
	}
	// Keep original acceptance uncommitted beyond its body deadline. The
	// observer must wait, then see the real accepted row rather than old absence.
	wait := time.NewTimer(max(time.Until(attempted.Deadline.Add(50*time.Millisecond)), time.Millisecond))
	defer wait.Stop()
	select {
	case <-wait.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	unblock.Do(func() { close(release) })
	result := <-accepted
	actual := <-observed
	if result.err != nil || actual.err != nil || actual.row.State != domain.StrategyActionWaiting || actual.row.IntentID != result.row.ID {
		t.Fatalf("late committed acceptance was replaced with a no-effect assertion: accepted=%+v/%v observed=%+v/%v", result.row, result.err, actual.row, actual.err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "99")
	assertGatewayOrderCount(ctx, t, db, 0)
}
