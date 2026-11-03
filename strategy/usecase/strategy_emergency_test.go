package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestStrategyEnrollsEmergencyScopeBeforeReadingOrdersAndWithoutActiveConfig(t *testing.T) {
	for _, version := range []int64{0, 1} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		port := &emergencyTickFixture{
			failure: domain.ErrOutcomeUnknown,
		}
		cancellations := &refusedManagementCancellation{}
		orders := &emergencyOrderReads{}
		controller := managementCancelController(cancellations, version)
		controller.deps.Execution = orders
		controller.deps.Emergencies = port
		_, err := controller.Tick(ctx)
		cancel()
		var diagnostic *domain.StrategyFailure
		if !errors.Is(err, domain.ErrOutcomeUnknown) || port.calls != 1 || port.strategyID != 7 || !errors.As(err, &diagnostic) || diagnostic.SafeStage() != string(domain.StrategyStageManagementEmergency) {
			t.Fatal("scope enrollment lost ordering, bounded stage or initial-config support", err)
		}
		if orders.calls != 0 || cancellations.calls != 0 {
			t.Fatal("unresolved scope enrollment reached order reads or cancellation dispatch")
		}
	}
}

func TestStrategyEmergencyCursorSchedulesOneDurableScopeAndResetsAtExhaustion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	port := &emergencyTickFixture{
		results: []domain.AdminEmergencyWork{
			{
				AcceptanceID: 11,
				State:        domain.AdminEmergencyCancelAccepted,
			},
			{},
			{
				AcceptanceID: 12,
				State:        domain.AdminEmergencyCancelSucceeded,
			},
		},
	}
	controller := managementCancelController(nil, 0)
	controller.deps.Emergencies = port
	for range 3 {
		if _, err := controller.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	want := []int64{0, 11, 0}
	if port.calls != len(want) || controller.afterEmergency != 12 {
		t.Fatal("tick drained an unbounded queue or lost the scope cursor")
	}
	for index := range want {
		if port.after[index] != want[index] {
			t.Fatal("scope cursor became authorization or skipped its exhausted restart")
		}
	}
}

func TestStrategyStandbyCannotEnrollEmergencyScopes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	port := &emergencyTickFixture{}
	controller := managementCancelController(nil, 0)
	controller.deps.Leases = &heldStrategyLease{}
	controller.deps.Emergencies = port
	if _, err := controller.Tick(ctx); err != nil || port.calls != 0 {
		t.Fatal("standby performed the active owner's management cycle", err)
	}
}

type emergencyTickFixture struct {
	calls      int
	strategyID int64
	after      []int64
	results    []domain.AdminEmergencyWork
	failure    error
}

type emergencyOrderReads struct {
	domain.StrategyExecution
	calls int
}

func (p *emergencyOrderReads) Orders(context.Context, domain.ExecutionFilter) ([]domain.IntentSnapshot, int64, error) {
	p.calls++
	return nil, 0, nil
}

func (p *emergencyTickFixture) Prepare(ctx context.Context, strategyID, afterID int64) (domain.AdminEmergencyWork, error) {
	if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil {
		return domain.AdminEmergencyWork{}, domain.ErrInvalidOrder
	}
	p.strategyID = strategyID
	p.after = append(p.after, afterID)
	p.calls++
	if len(p.results) >= p.calls {
		return p.results[p.calls-1], p.failure
	}
	return domain.AdminEmergencyWork{}, p.failure
}
