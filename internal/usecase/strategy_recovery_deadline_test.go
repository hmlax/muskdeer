package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyLocalRecoveryDeadlineRetainsActionAndWaitsForFreshTick(t *testing.T) {
	for _, port := range []string{"active", "discard", "observe", "transition"} {
		t.Run(port, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			actions, execution := remoteRiskFixture(domain.StrategyActionSubmit, nil)
			if port == "observe" {
				actions.row.State = domain.StrategyActionDispatching
			}
			guard := &expiringRecoveryActions{
				remoteRiskActions: actions,
				port:              port,
			}
			controller := &StrategyController{deps: StrategyDependencies{
				Actions:   guard,
				Execution: execution,
				Lifecycle: &expiringRecoveryLifecycle{},
			}}
			var err error
			if port == "transition" {
				_, err = controller.transition(ctx, domain.StrategySpec{}, domain.LeaseToken{}, domain.StrategyStateRecovering, false)
			} else {
				_, err = controller.recoverActions(ctx, domain.StrategySpec{ID: 1}, domain.StrategyAuthority{})
			}
			if !errors.Is(err, errStrategyReobserve) || ctx.Err() != context.DeadlineExceeded || execution.submits != 0 || actions.row.IdempotencyKey != "original-stable-key" {
				t.Fatal("local recovery deadline killed worker, replaced identity or sent another command", err)
			}
		})
	}
}

func TestStrategyLocalRecoveryDeadlinePreservesCriticalFailures(t *testing.T) {
	for _, cause := range []error{domain.ErrLeaseLost, domain.ErrForbidden, domain.ErrInvalidParameters, domain.ErrInvalidOrder, context.Canceled} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		controller := &StrategyController{deps: StrategyDependencies{Lifecycle: &expiringRecoveryLifecycle{cause: cause}}}
		_, err := controller.transition(ctx, domain.StrategySpec{}, domain.LeaseToken{}, domain.StrategyStateRecovering, false)
		cancel()
		if !errors.Is(err, cause) || errors.Is(err, errStrategyReobserve) {
			t.Fatal("expired transition hid current authority or data failure", err)
		}
	}
}

type expiringRecoveryActions struct {
	*remoteRiskActions
	port string
}

func (a *expiringRecoveryActions) Active(ctx context.Context, id int64) ([]domain.StrategyAction, error) {
	if a.port == "active" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return a.remoteRiskActions.Active(ctx, id)
}

func (*expiringRecoveryActions) Discard(ctx context.Context, _ domain.StrategyAction, _ domain.StrategyAuthority) (domain.StrategyAction, error) {
	<-ctx.Done()
	return domain.StrategyAction{}, ctx.Err()
}

func (*expiringRecoveryActions) Observe(ctx context.Context, _ domain.StrategyAction, _ domain.StrategyAuthority) (domain.StrategyAction, error) {
	<-ctx.Done()
	return domain.StrategyAction{}, ctx.Err()
}

type expiringRecoveryLifecycle struct {
	domain.StrategyLifecycle
	cause error
}

func (l *expiringRecoveryLifecycle) Transition(ctx context.Context, _ domain.StrategySpec, _ domain.LeaseToken, _ domain.StrategyTransition) (domain.StrategySpec, error) {
	<-ctx.Done()
	return domain.StrategySpec{}, errors.Join(l.cause, ctx.Err())
}
