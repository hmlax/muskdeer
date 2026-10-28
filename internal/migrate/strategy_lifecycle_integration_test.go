//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyLifecycleRecoversBeforeRunningAndWaitsForTerminalStop(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
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
	if _, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateRunning}); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("a new incarnation skipped recovery")
	}
	recovering, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateRecovering})
	if err != nil || recovering.RuntimeOwnerEpoch != token.Epoch || recovering.State != domain.StrategyStateRecovering {
		t.Fatalf("takeover was not persisted as recovering: %+v, %v", recovering, err)
	}
	if _, err := lifecycle.Transition(ctx, recovering, token, domain.StrategyTransition{
		State:             domain.StrategyStateRunning,
		ActivateRequested: true,
	}); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatal("missing portfolio allowed a runnable transition")
	}
	portfolioFixture(ctx, t, db, submit)
	feed, publisher, snapshots, publication := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := snapshots.Publish(ctx, publisher, publication.Revision, marketReadingFixture(feed, publication.Reading.Sequence+1, "100")); err != nil {
		t.Fatal(err)
	}
	running, err := lifecycle.Transition(ctx, recovering, token, domain.StrategyTransition{
		State:             domain.StrategyStateRunning,
		ActivateRequested: true,
	})
	if err != nil || running.State != domain.StrategyStateRunning || running.ActiveVersion != running.RequestedVersion {
		t.Fatalf("ready recovery did not activate the requested pair: %+v, %v", running, err)
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", running.ID).First(&risk).Error; err != nil {
		t.Fatal(err)
	}
	if risk.AllowNewRisk {
		t.Fatal("lifecycle transition granted risk without execution evaluation")
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	if decision, err := evaluator.Evaluate(ctx, running.ID); err != nil || !decision.Allowed {
		t.Fatalf("execution evaluation did not confirm ready runtime: %+v, %v", decision, err)
	}
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatalf("runtime test order did not become live: %+v, %v", live, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", running.ID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	current, err = lifecycle.Instance(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateStopped}); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("live order was reported as a completed stop")
	}
	stopping, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateStopping})
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "lifecycle-stop"), submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil {
		t.Fatal(err)
	}
	stopped, err := lifecycle.Transition(ctx, stopping, token, domain.StrategyTransition{State: domain.StrategyStateStopped})
	if err != nil || stopped.State != domain.StrategyStateStopped || stopped.ActiveVersion != running.ActiveVersion {
		t.Fatalf("terminal stop lost retained policy or failed: %+v, %v", stopped, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func lifecycleToken(authority domain.StrategyAuthority) domain.LeaseToken {
	return domain.LeaseToken{
		Key: domain.LeaseKey{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: authority.StrategyID,
		},
		OwnerID: authority.OwnerID,
		Epoch:   authority.OwnerEpoch,
	}
}
