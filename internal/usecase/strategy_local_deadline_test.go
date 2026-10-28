package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyExpiredLocalAdmissionWaitsForFreshObservation(t *testing.T) {
	for _, attempt := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		actions := &expiredLocalActions{atAttempt: attempt}
		controller := &StrategyController{deps: StrategyDependencies{Actions: actions}}
		err := controller.planAndDispatch(ctx, domain.StrategyAuthority{}, domain.StrategyActionPlan{Kind: domain.StrategyActionSubmit})
		contextErr := ctx.Err()
		cancel()
		if !errors.Is(err, errStrategyReobserve) || contextErr != context.DeadlineExceeded || actions.plans != 1 || actions.attempts != boolCount(attempt) {
			t.Fatalf("expired local admission killed worker or dispatched again: attempt=%t plans=%d attempts=%d error=%v", attempt, actions.plans, actions.attempts, err)
		}
	}
}

func TestStrategyExpiredLocalAdmissionPreservesCriticalJoinedErrors(t *testing.T) {
	for _, cause := range []error{domain.ErrLeaseLost, domain.ErrInvalidOrder, domain.ErrIdempotencyConflict, domain.ErrInvalidParameters, context.Canceled} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		actions := &expiredLocalActions{cause: cause}
		controller := &StrategyController{deps: StrategyDependencies{Actions: actions}}
		err := controller.planAndDispatch(ctx, domain.StrategyAuthority{}, domain.StrategyActionPlan{Kind: domain.StrategyActionSubmit})
		cancel()
		if !errors.Is(err, cause) || errors.Is(err, errStrategyReobserve) {
			t.Fatal("actual deadline hid a critical failure")
		}
	}
}

type expiredLocalActions struct {
	domain.StrategyActions
	atAttempt bool
	plans     int
	attempts  int
	cause     error
}

func (a *expiredLocalActions) Plan(ctx context.Context, _ domain.StrategyAuthority, plan domain.StrategyActionPlan) (domain.StrategyAction, error) {
	a.plans++
	if !a.atAttempt {
		<-ctx.Done()
		return domain.StrategyAction{}, errors.Join(a.cause, ctx.Err())
	}
	return domain.StrategyAction{Plan: plan}, nil
}

func (a *expiredLocalActions) Attempt(ctx context.Context, _ domain.StrategyAction, _ domain.StrategyAuthority, _ time.Time) (domain.StrategyAction, error) {
	a.attempts++
	<-ctx.Done()
	return domain.StrategyAction{}, errors.Join(a.cause, ctx.Err())
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
