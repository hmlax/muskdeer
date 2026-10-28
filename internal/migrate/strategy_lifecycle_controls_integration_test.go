//go:build integration

package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyLifecycleHoldsUnresolvedActionsAndFencesNewIncarnation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := actions.Plan(ctx, submit.Input.Authority, journalSubmitPlan(submit, "before-recovery"))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.Instance(ctx, submit.Input.Authority.StrategyID)
	if err != nil {
		t.Fatal(err)
	}
	first := lifecycleToken(submit.Input.Authority)
	recovering, err := lifecycle.Transition(ctx, current, first, domain.StrategyTransition{State: domain.StrategyStateRecovering})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(ctx, recovering, first, domain.StrategyTransition{State: domain.StrategyStateRunning}); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("unresolved action allowed a runnable state")
	}
	if _, err := actions.Discard(ctx, pending, submit.Input.Authority); err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, first.Key)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "lifecycle-replacement")
	if err != nil {
		t.Fatal(err)
	}
	second, err := leases.Acquire(ctx, first.Key, owner, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(ctx, recovering, second, domain.StrategyTransition{State: domain.StrategyStateRunning}); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("replacement incarnation skipped its own recovery")
	}
	replaced, err := lifecycle.Transition(ctx, recovering, second, domain.StrategyTransition{State: domain.StrategyStateRecovering})
	if err != nil || replaced.RuntimeOwnerEpoch <= recovering.RuntimeOwnerEpoch || replaced.RuntimeOwnerID != second.OwnerID {
		t.Fatalf("runtime incarnation did not advance: %+v, %v", replaced, err)
	}
	if _, err := lifecycle.Transition(ctx, replaced, first, domain.StrategyTransition{State: domain.StrategyStateRecovering}); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("former runtime owner changed successor state")
	}
	err = db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", replaced.ID).UpdateColumn("runtime_owner_epoch", 0).Error
	assertDatabaseCode(t, err, "23514")
}

func TestStrategyLifecycleCommitFenceRollsBackStagedState(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.Instance(ctx, submit.Input.Authority.StrategyID)
	if err != nil {
		t.Fatal(err)
	}
	token := lifecycleToken(submit.Input.Authority)
	recovering, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateRecovering})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", recovering.ID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "test:expire-strategy-lifecycle-authority"
	if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeFor[modelv1.StrategyInstance]() {
			return
		}
		result := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.StrategyLease{}).
			Where("strategy_instance_id = ?", recovering.ID).UpdateColumn("expires_at", 0)
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
	if _, err := lifecycle.Transition(ctx, recovering, token, domain.StrategyTransition{State: domain.StrategyStateStopping}); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("staged lifecycle state escaped final authority check: %v", err)
	}
	retained, err := lifecycle.Instance(ctx, recovering.ID)
	if err != nil || retained.State != domain.StrategyStateRecovering || retained.Revision != recovering.Revision {
		t.Fatalf("failed lifecycle fence left staged state: %+v, %v", retained, err)
	}
}
