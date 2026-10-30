package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyProcessesManagementCancellationBeforeReadingQuoteInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	port := &refusedManagementCancellation{}
	controller := managementCancelController(port, 1)
	_, err := controller.Tick(ctx)
	if !errors.Is(err, domain.ErrLeaseLost) || port.calls != 1 {
		t.Fatalf("Tick ignored management cancellation fencing: calls=%d error=%v", port.calls, err)
	}
}

func TestStrategyFirstActivationDoesNotUseMissingCancellationAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	port := &refusedManagementCancellation{}
	controller := managementCancelController(port, 0)
	if _, err := controller.Tick(ctx); err != nil || port.calls != 0 {
		t.Fatal("first activation tried to cancel before any active configuration existed", err)
	}
}

func managementCancelController(port domain.StrategyOrderCancellations, version int64) *StrategyController {
	controller := &StrategyController{
		deps: StrategyDependencies{
			Leases:        &managementCancelLease{},
			Lifecycle:     &managementCancelLifecycle{activeVersion: version},
			Actions:       &managementCancelActions{},
			Execution:     &managementCancelExecution{},
			Cancellations: port,
		},
		settings: StrategySettings{
			StrategyID: 7,
			OwnerID:    "management-owner",
			LeaseTTL:   5 * time.Second,
		},
	}
	return controller
}

type refusedManagementCancellation struct{ calls int }

func (p *refusedManagementCancellation) Prepare(context.Context, domain.StrategyAuthority, int64) (domain.AdminCancellationWork, error) {
	p.calls++
	return domain.AdminCancellationWork{}, domain.ErrLeaseLost
}

type managementCancelLease struct{ domain.LeaseRepository }

func (*managementCancelLease) Acquire(context.Context, domain.LeaseKey, string, time.Duration) (domain.LeaseToken, error) {
	return domain.LeaseToken{OwnerID: "management-owner"}, nil
}

func (*managementCancelLease) Renew(_ context.Context, current domain.LeaseToken, _ time.Duration) (domain.LeaseToken, error) {
	return current, nil
}

type managementCancelLifecycle struct {
	domain.StrategyLifecycle
	activeVersion int64
}

func (l *managementCancelLifecycle) Instance(context.Context, int64) (domain.StrategySpec, error) {
	return domain.StrategySpec{
		ID:             7,
		RuntimeOwnerID: "management-owner",
		State:          domain.StrategyStateStopped,
		DesiredState:   domain.StrategyStateStopped,
		ActiveVersion:  l.activeVersion,
	}, nil
}

func (*managementCancelLifecycle) Transition(_ context.Context, current domain.StrategySpec, _ domain.LeaseToken, _ domain.StrategyTransition) (domain.StrategySpec, error) {
	return current, nil
}

type managementCancelActions struct{ domain.StrategyActions }

func (*managementCancelActions) Active(context.Context, int64) ([]domain.StrategyAction, error) {
	return nil, nil
}

type managementCancelExecution struct{ domain.StrategyExecution }

func (*managementCancelExecution) Orders(context.Context, domain.ExecutionFilter) ([]domain.IntentSnapshot, int64, error) {
	return nil, 0, nil
}
