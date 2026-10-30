package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/requestid"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyActionKeepsBusinessKeysAndUsesValidRPCTracing(t *testing.T) {
	for _, kind := range []domain.StrategyActionKind{domain.StrategyActionCancel, domain.StrategyActionSubmit} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		pending := domain.StrategyAction{
			ID:             7,
			IdempotencyKey: "original-business-idempotency-key",
			Plan: domain.StrategyActionPlan{
				Key:  domain.AdminCancellationDecisionPrefix + "7",
				Kind: kind,
			},
		}
		actions := &requestTraceActions{}
		execution := &requestTraceExecution{}
		controller := &StrategyController{deps: StrategyDependencies{
			Actions:   actions,
			Execution: execution,
		}}
		err := controller.dispatch(ctx, pending, domain.StrategyAuthority{})
		cancel()
		if err != nil || execution.key != pending.IdempotencyKey || execution.trace == pending.Plan.Key || actions.observed != 1 {
			t.Fatal("business decision key was used as invalid RPC tracing or business identity changed", kind, err)
		}
	}
}

type requestTraceActions struct {
	domain.StrategyActions
	observed int
}

func (*requestTraceActions) Attempt(_ context.Context, value domain.StrategyAction, _ domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	value.Deadline = deadline
	return value, nil
}

func (a *requestTraceActions) Observe(_ context.Context, value domain.StrategyAction, _ domain.StrategyAuthority) (domain.StrategyAction, error) {
	a.observed++
	return value, nil
}

type requestTraceExecution struct {
	domain.StrategyExecution
	key   string
	trace string
}

func (e *requestTraceExecution) Submit(ctx context.Context, input domain.OrderInput) (domain.IntentSnapshot, error) {
	return domain.IntentSnapshot{}, e.validate(ctx, input.RequestID, input.IdempotencyKey)
}

func (e *requestTraceExecution) Cancel(ctx context.Context, input domain.CancelInput) (domain.CancellationResult, error) {
	return domain.CancellationResult{}, e.validate(ctx, input.RequestID, input.IdempotencyKey)
}

func (e *requestTraceExecution) validate(ctx context.Context, trace, key string) error {
	e.trace, e.key = trace, key
	canonical, err := requestid.New(ctx, trace)
	if err != nil {
		return err
	}
	if canonical.String() != trace {
		return domain.ErrInvalidOrder
	}
	return nil
}
