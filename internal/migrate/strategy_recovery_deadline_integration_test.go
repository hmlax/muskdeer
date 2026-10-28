//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestStrategyRecoveryCommitDeadlineKeepsProofAndStopsWithoutResubmit(t *testing.T) {
	for _, observe := range []bool{false, true} {
		name := "committed_transition"
		if observe {
			name = "committed_observation"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			portfolioFixture(ctx, t, db, submit)
			_, execution, market := strategyControllerFixture(ctx, t, db, submit)
			controller, guard := recoveryDeadlineController(ctx, t, db, submit, execution, market, observe)
			for range 8 {
				strategyControllerTick(ctx, t, controller, market)
				if guard.fired {
					break
				}
			}
			if !guard.fired || execution.submits != recoveryDeadlineSubmits(observe) {
				t.Fatal("committed local recovery deadline lost or duplicated a remote effect")
			}
			assertRecoveryDeadlineProof(ctx, t, guard, submit.Input.Authority.StrategyID)
			if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
				t.Fatal(err)
			}
			for range 8 {
				strategyControllerTick(ctx, t, controller, market)
			}
			orders, _, err := execution.Orders(ctx, domain.ExecutionFilter{
				AccountID:    submit.Input.AccountID,
				StrategyID:   submit.Input.Authority.StrategyID,
				InstrumentID: submit.Input.InstrumentID,
				Limit:        domain.MaximumExecutionPage,
				OpenOnly:     true,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, order := range orders {
				if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
					t.Fatal(err)
				}
			}
			for range 8 {
				strategyControllerTick(ctx, t, controller, market)
			}
			current, err := guard.Instance(ctx, submit.Input.Authority.StrategyID)
			if err != nil || current.State != domain.StrategyStateStopped || execution.submits != recoveryDeadlineSubmits(observe) {
				t.Fatal("fresh recovery failed to stop or resubmitted the original effect", err)
			}
			assertGatewayOrderCount(ctx, t, db, int64(recoveryDeadlineSubmits(observe)))
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
		})
	}
}

func recoveryDeadlineController(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, execution domain.StrategyExecution, market domain.MarketReader, observe bool) (*usecase.StrategyController, *committedRecoveryDeadline) {
	t.Helper()
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := repository.NewStrategyEnvironment(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "recovery-commit-deadline")
	if err != nil {
		t.Fatal(err)
	}
	guard := &committedRecoveryDeadline{
		StrategyLifecycle: lifecycle,
		StrategyActions:   actions,
		observe:           observe,
	}
	controller, err := usecase.NewStrategyController(usecase.StrategyDependencies{
		Leases:      leases,
		Lifecycle:   guard,
		Environment: environment,
		Actions:     guard,
		Execution:   execution,
		Market:      market,
	}, usecase.StrategySettings{
		StrategyID: submit.Input.Authority.StrategyID,
		OwnerID:    owner,
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller, guard
}

func assertRecoveryDeadlineProof(ctx context.Context, t *testing.T, guard *committedRecoveryDeadline, id int64) {
	t.Helper()
	if !guard.observe {
		current, err := guard.Instance(ctx, id)
		if err != nil || current.Revision != guard.committedSpec.Revision || current.RuntimeOwnerID != guard.committedSpec.RuntimeOwnerID || current.RuntimeOwnerEpoch != guard.committedSpec.RuntimeOwnerEpoch || current.RuntimeOwnerEpoch <= 0 {
			t.Fatal("committed lifecycle owner/revision was lost", err)
		}
		return
	}
	current, err := guard.ByKey(ctx, id, guard.committedAction.Plan.Key)
	if err != nil || current.ID != guard.committedAction.ID || current.IdempotencyKey != guard.committedAction.IdempotencyKey || current.State != guard.committedAction.State || !current.Deadline.Equal(guard.committedAction.Deadline) {
		t.Fatal("committed observation identity/deadline was lost", err)
	}
}

func recoveryDeadlineSubmits(observe bool) int {
	if observe {
		return 1
	}
	return 0
}

type committedRecoveryDeadline struct {
	domain.StrategyLifecycle
	domain.StrategyActions
	observe         bool
	fired           bool
	committedSpec   domain.StrategySpec
	committedAction domain.StrategyAction
}

func (g *committedRecoveryDeadline) Transition(ctx context.Context, current domain.StrategySpec, token domain.LeaseToken, transition domain.StrategyTransition) (domain.StrategySpec, error) {
	result, err := g.StrategyLifecycle.Transition(ctx, current, token, transition)
	if err == nil && !g.observe && !g.fired {
		g.fired, g.committedSpec = true, result
		<-ctx.Done()
		return result, ctx.Err()
	}
	return result, err
}

func (g *committedRecoveryDeadline) Observe(ctx context.Context, action domain.StrategyAction, authority domain.StrategyAuthority) (domain.StrategyAction, error) {
	result, err := g.StrategyActions.Observe(ctx, action, authority)
	if err == nil && g.observe && !g.fired {
		g.fired, g.committedAction = true, result
		<-ctx.Done()
		return result, ctx.Err()
	}
	return result, err
}
