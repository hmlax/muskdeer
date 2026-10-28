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

func TestStrategyStopBetweenQuoteAndActionAdmissionReobservesWithoutWrite(t *testing.T) {
	for _, beforeAttempt := range []bool{false, true} {
		name := "before_plan"
		if beforeAttempt {
			name = "before_attempt"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			portfolioFixture(ctx, t, db, submit)
			_, execution, market := strategyControllerFixture(ctx, t, db, submit)
			leases, err := repository.NewLeases(db)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err := repository.NewStrategyLifecycle(db)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := repository.NewStrategyEnvironment(db)
			if err != nil {
				t.Fatal(err)
			}
			actions, err := repository.NewStrategyActions(db)
			if err != nil {
				t.Fatal(err)
			}
			guard := &stopBeforeStrategyAction{
				StrategyActions: actions,
				db:              db,
				strategyID:      submit.Input.Authority.StrategyID,
				beforeAttempt:   beforeAttempt,
			}
			owner, err := ownerid.New(ctx, "stop-admission")
			if err != nil {
				t.Fatal(err)
			}
			controller, err := usecase.NewStrategyController(usecase.StrategyDependencies{
				Leases:      leases,
				Lifecycle:   lifecycle,
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
			for range 10 {
				strategyControllerTick(ctx, t, controller, market)
			}
			state, err := lifecycle.Instance(ctx, submit.Input.Authority.StrategyID)
			if err != nil || !guard.stopped || state.DesiredRunning || state.State != domain.StrategyStateStopped || execution.submits != 0 || execution.cancels != 0 {
				t.Fatalf("stop admission race failed to converge without a write: fired=%t state=%s submit=%d cancel=%d err=%v", guard.stopped, state.State, execution.submits, execution.cancels, err)
			}
			active, err := actions.Active(ctx, state.ID)
			if err != nil || len(active) != 0 {
				t.Fatal("unsent action was not discarded during stop")
			}
			assertGatewayOrderCount(ctx, t, db, 0)
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
		})
	}
}

type stopBeforeStrategyAction struct {
	domain.StrategyActions
	db            *gorm.DB
	strategyID    int64
	beforeAttempt bool
	stopped       bool
}

func (s *stopBeforeStrategyAction) Plan(ctx context.Context, authority domain.StrategyAuthority, plan domain.StrategyActionPlan) (domain.StrategyAction, error) {
	if !s.beforeAttempt && plan.Kind == domain.StrategyActionSubmit {
		if err := s.stop(ctx); err != nil {
			return domain.StrategyAction{}, err
		}
	}
	return s.StrategyActions.Plan(ctx, authority, plan)
}

func (s *stopBeforeStrategyAction) Attempt(ctx context.Context, action domain.StrategyAction, authority domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	if s.beforeAttempt && action.Plan.Kind == domain.StrategyActionSubmit {
		if err := s.stop(ctx); err != nil {
			return domain.StrategyAction{}, err
		}
	}
	return s.StrategyActions.Attempt(ctx, action, authority, deadline)
}

func (s *stopBeforeStrategyAction) stop(ctx context.Context) error {
	if s.stopped {
		return nil
	}
	result := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", s.strategyID).
		UpdateColumn("desired_running", false)
	if result.Error != nil {
		return result.Error
	}
	s.stopped = true
	return nil
}
