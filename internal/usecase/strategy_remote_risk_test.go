package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyRemoteRiskRefusalKeepsAttemptForOriginalKeyObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	actions, execution := remoteRiskFixture(domain.StrategyActionSubmit, domain.ErrRiskDenied)
	controller := &StrategyController{deps: StrategyDependencies{
		Actions:   actions,
		Execution: execution,
	}}
	if err := controller.dispatch(ctx, actions.row, domain.StrategyAuthority{}); !errors.Is(err, errStrategyReobserve) {
		t.Fatal("remote risk refusal killed the controller instead of retaining its attempt")
	}
	if actions.attempts != 1 || execution.submits != 1 || actions.observations != 0 || actions.row.State != domain.StrategyActionDispatching {
		t.Fatal("refusal replayed, discarded or prematurely resolved an attempted operation")
	}
	if _, err := controller.recoverActions(ctx, domain.StrategySpec{ID: 1}, domain.StrategyAuthority{}); err != nil {
		t.Fatal(err)
	}
	if execution.submits != 1 || execution.finds != 1 || actions.observations != 1 || execution.lookupKey != actions.row.IdempotencyKey || actions.row.State != domain.StrategyActionUnknown {
		t.Fatal("fresh recovery failed to observe only the original stable key")
	}
}

func TestStrategyRemoteRefusalDoesNotHideOtherFailuresOrCancellationErrors(t *testing.T) {
	for _, cause := range []error{domain.ErrLeaseLost, domain.ErrInvalidOrder, domain.ErrIdempotencyConflict, context.DeadlineExceeded} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		actions, execution := remoteRiskFixture(domain.StrategyActionSubmit, cause)
		controller := &StrategyController{deps: StrategyDependencies{
			Actions:   actions,
			Execution: execution,
		}}
		err := controller.dispatch(ctx, actions.row, domain.StrategyAuthority{})
		cancel()
		if !errors.Is(err, cause) || errors.Is(err, errStrategyReobserve) {
			t.Fatal("non-risk execution failure was hidden")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	actions, execution := remoteRiskFixture(domain.StrategyActionCancel, domain.ErrRiskDenied)
	controller := &StrategyController{deps: StrategyDependencies{
		Actions:   actions,
		Execution: execution,
	}}
	if err := controller.dispatch(ctx, actions.row, domain.StrategyAuthority{}); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatal("unexpected cancellation refusal was treated as new-risk admission")
	}
}

type remoteRiskActions struct {
	domain.StrategyActions
	row          domain.StrategyAction
	attempts     int
	observations int
}

func (a *remoteRiskActions) Attempt(_ context.Context, _ domain.StrategyAction, _ domain.StrategyAuthority, deadline time.Time) (domain.StrategyAction, error) {
	a.attempts++
	a.row.State, a.row.Deadline = domain.StrategyActionDispatching, deadline
	return a.row, nil
}

func (a *remoteRiskActions) Active(context.Context, int64) ([]domain.StrategyAction, error) {
	return []domain.StrategyAction{a.row}, nil
}

func (a *remoteRiskActions) Observe(context.Context, domain.StrategyAction, domain.StrategyAuthority) (domain.StrategyAction, error) {
	a.observations++
	a.row.State = domain.StrategyActionUnknown
	return a.row, nil
}

type remoteRiskExecution struct {
	domain.StrategyExecution
	failure   error
	submits   int
	finds     int
	lookupKey string
}

func (e *remoteRiskExecution) Submit(context.Context, domain.OrderInput) (domain.IntentSnapshot, error) {
	e.submits++
	return domain.IntentSnapshot{}, e.failure
}

func (e *remoteRiskExecution) Cancel(context.Context, domain.CancelInput) (domain.CancellationResult, error) {
	return domain.CancellationResult{}, e.failure
}

func (e *remoteRiskExecution) FindOrder(_ context.Context, _ string, key string) (domain.IntentSnapshot, error) {
	e.finds++
	e.lookupKey = key
	return domain.IntentSnapshot{}, domain.ErrCoreNotFound
}

func remoteRiskFixture(kind domain.StrategyActionKind, failure error) (*remoteRiskActions, *remoteRiskExecution) {
	return &remoteRiskActions{row: domain.StrategyAction{
		ID:             1,
		StrategyID:     1,
		IdempotencyKey: "original-stable-key",
		State:          domain.StrategyActionPending,
		Plan: domain.StrategyActionPlan{
			Key:       "decision-key",
			AccountID: "test-account",
			Kind:      kind,
		},
	}}, &remoteRiskExecution{failure: failure}
}
