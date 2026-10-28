package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyExpiredRemoteAttemptWaitsForFreshObservation(t *testing.T) {
	for _, kind := range []domain.StrategyActionKind{domain.StrategyActionSubmit, domain.StrategyActionCancel} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			actions := &deadlineStrategyActions{}
			execution := &deadlineStrategyExecution{}
			controller := &StrategyController{deps: StrategyDependencies{
				Actions:   actions,
				Execution: execution,
			}}
			err := controller.dispatch(ctx, domain.StrategyAction{Plan: domain.StrategyActionPlan{
				Kind: kind,
			}}, domain.StrategyAuthority{})
			if !errors.Is(err, errStrategyReobserve) || actions.attempts != 1 || execution.writes != 1 || actions.observations != 0 {
				t.Fatal("expired remote attempt queried with a canceled context or lost its durable attempt")
			}
		})
	}
}

func TestStrategyExpiredReadDoesNotAttemptAnotherDatabaseObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	actions := &deadlineStrategyActions{}
	controller := &StrategyController{deps: StrategyDependencies{
		Actions:   actions,
		Execution: &deadlineStrategyExecution{},
	}}
	err := controller.observeAction(ctx, domain.StrategyAction{Plan: domain.StrategyActionPlan{
		Kind: domain.StrategyActionSubmit,
	}}, domain.StrategyAuthority{})
	if !errors.Is(err, errStrategyReobserve) || actions.observations != 0 || actions.attempts != 0 {
		t.Fatal("expired read consumed another operation or authorized redispatch")
	}
}

func TestStrategyExpiredMarketReadWaitsForFreshProtectionContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	controller := &StrategyController{deps: StrategyDependencies{
		Environment: &deadlineStrategyEnvironment{},
		Market:      &deadlineStrategyMarket{},
		Lifecycle:   &unusedStrategyLifecycle{},
	}}
	// Lifecycle deliberately panics if protection is attempted using the
	// expired market call's context. The next tick gets a fresh risk snapshot.
	if err := controller.quote(ctx, domain.StrategySpec{}, domain.LeaseToken{}, nil); !errors.Is(err, errStrategyReobserve) {
		t.Fatal("expired market read tried to protect with an exhausted context")
	}
}

type deadlineStrategyEnvironment struct{}

func (*deadlineStrategyEnvironment) Current(context.Context, domain.StrategySpec) (domain.StrategyEnvironmentView, error) {
	return domain.StrategyEnvironmentView{
		RiskAllowed: true,
		Parameters: domain.StrategyParameters{
			Template: domain.StrategyTemplateMarketMaker,
			Maker:    &domain.MakerParameters{},
		},
	}, nil
}

type deadlineStrategyMarket struct{}

func (*deadlineStrategyMarket) Reference(ctx context.Context, _ domain.MarketSource, _ int64, _ domain.MarketMode) (domain.MarketReference, error) {
	<-ctx.Done()
	return domain.MarketReference{}, ctx.Err()
}

type deadlineStrategyActions struct {
	domain.StrategyActions
	attempts     int
	observations int
}

func (a *deadlineStrategyActions) Attempt(_ context.Context, action domain.StrategyAction, _ domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	a.attempts++
	action.State, action.Deadline = domain.StrategyActionDispatching, deadline
	return action, nil
}

func (a *deadlineStrategyActions) Observe(context.Context, domain.StrategyAction, domain.StrategyAuthority) (domain.StrategyAction, error) {
	a.observations++
	return domain.StrategyAction{}, context.DeadlineExceeded
}

type deadlineStrategyExecution struct {
	domain.StrategyExecution
	writes int
}

func (e *deadlineStrategyExecution) Submit(ctx context.Context, _ domain.OrderInput) (domain.IntentSnapshot, error) {
	e.writes++
	<-ctx.Done()
	return domain.IntentSnapshot{}, domain.ErrOutcomeUnknown
}

func (e *deadlineStrategyExecution) Cancel(ctx context.Context, _ domain.CancelInput) (domain.CancellationResult, error) {
	e.writes++
	<-ctx.Done()
	return domain.CancellationResult{}, domain.ErrOutcomeUnknown
}

func (*deadlineStrategyExecution) FindOrder(ctx context.Context, _, _ string) (domain.IntentSnapshot, error) {
	<-ctx.Done()
	return domain.IntentSnapshot{}, domain.ErrOutcomeUnknown
}
