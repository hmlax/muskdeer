//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestStrategyLocalCommitDeadlineRetainsJournalAndStopsWithoutRemoteWrite(t *testing.T) {
	for _, attempt := range []bool{false, true} {
		name := "committed_plan"
		if attempt {
			name = "committed_attempt"
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
			guard := &expireAfterLocalCommit{
				StrategyActions: actions,
				atAttempt:       attempt,
			}
			owner, err := ownerid.New(ctx, "local-commit-deadline")
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
			for range 8 {
				strategyControllerTick(ctx, t, controller, market)
				if guard.fired {
					break
				}
			}
			if !guard.fired || execution.submits != 0 || execution.cancels != 0 {
				t.Fatal("local commit deadline reached a remote write")
			}
			active, err := actions.Active(ctx, submit.Input.Authority.StrategyID)
			if err != nil || len(active) != 1 || active[0].ID != guard.committed.ID || active[0].Plan.Key != guard.committed.Plan.Key || active[0].State != guard.committed.State {
				t.Fatal("deadline erased or replaced the committed journal", err)
			}
			assertGatewayOrderCount(ctx, t, db, 0)
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", active[0].StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
				t.Fatal(err)
			}
			for range 8 {
				strategyControllerTick(ctx, t, controller, market)
			}
			resolved, err := actions.ByKey(ctx, active[0].StrategyID, active[0].Plan.Key)
			expected := domain.StrategyActionDiscarded
			if attempt {
				expected = domain.StrategyActionNotAccepted
			}
			if err != nil || resolved.ID != active[0].ID || resolved.IdempotencyKey != active[0].IdempotencyKey || resolved.State != expected || execution.submits != 0 {
				t.Fatal("fresh observation resent or misclassified the original action", err)
			}
			state, err := lifecycle.Instance(ctx, active[0].StrategyID)
			if err != nil || state.State != domain.StrategyStateStopped {
				t.Fatal("local timeout prevented terminal stop", err)
			}
			assertGatewayOrderCount(ctx, t, db, 0)
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
		})
	}
}

type expireAfterLocalCommit struct {
	domain.StrategyActions
	atAttempt bool
	fired     bool
	committed domain.StrategyAction
}

func (s *expireAfterLocalCommit) Plan(ctx context.Context, authority domain.StrategyAuthority, plan domain.StrategyActionPlan) (domain.StrategyAction, error) {
	action, err := s.StrategyActions.Plan(ctx, authority, plan)
	if err == nil && !s.atAttempt && !s.fired {
		s.fired, s.committed = true, action
		<-ctx.Done()
		return action, ctx.Err()
	}
	return action, err
}

func (s *expireAfterLocalCommit) Attempt(ctx context.Context, action domain.StrategyAction, authority domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	result, err := s.StrategyActions.Attempt(ctx, action, authority, deadline)
	if err == nil && s.atAttempt && !s.fired {
		s.fired, s.committed = true, result
		<-ctx.Done()
		return result, ctx.Err()
	}
	return result, err
}
