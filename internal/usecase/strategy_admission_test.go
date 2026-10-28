package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyLocalAdmissionRefusalWaitsButRealFailuresPropagate(t *testing.T) {
	for _, atAttempt := range []bool{false, true} {
		for _, cause := range []error{domain.ErrRiskDenied, domain.ErrLeaseLost, domain.ErrInvalidOrder, domain.ErrIdempotencyConflict, context.DeadlineExceeded} {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			actions := &refusedStrategyActions{
				atAttempt: atAttempt,
				cause:     cause,
			}
			// No execution dependency: any write after a local refusal would panic.
			controller := &StrategyController{deps: StrategyDependencies{Actions: actions}}
			err := controller.planAndDispatch(ctx, domain.StrategyAuthority{}, domain.StrategyActionPlan{Kind: domain.StrategyActionSubmit})
			cancel()
			if cause == domain.ErrRiskDenied {
				if !errors.Is(err, errStrategyReobserve) {
					t.Fatal("local refusal killed the worker")
				}
				continue
			}
			if !errors.Is(err, cause) || errors.Is(err, errStrategyReobserve) {
				t.Fatal("real failure was hidden by admission handling")
			}
		}
	}
}

type refusedStrategyActions struct {
	domain.StrategyActions
	atAttempt bool
	cause     error
}

func (a *refusedStrategyActions) Plan(_ context.Context, _ domain.StrategyAuthority, plan domain.StrategyActionPlan) (domain.StrategyAction, error) {
	if !a.atAttempt {
		return domain.StrategyAction{}, a.cause
	}
	return domain.StrategyAction{Plan: plan}, nil
}

func (a *refusedStrategyActions) Attempt(context.Context, domain.StrategyAction, domain.StrategyAuthority, time.Time) (domain.StrategyAction, error) {
	return domain.StrategyAction{}, a.cause
}
