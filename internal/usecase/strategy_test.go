package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

type heldStrategyLease struct {
	domain.LeaseRepository
	calls int
}

func (l *heldStrategyLease) Acquire(context.Context, domain.LeaseKey, string, time.Duration) (domain.LeaseToken, error) {
	l.calls++
	return domain.LeaseToken{}, domain.ErrLeaseHeld
}

type unusedStrategyLifecycle struct{ domain.StrategyLifecycle }
type unusedStrategyEnvironment struct{ domain.StrategyEnvironment }
type unusedStrategyActions struct{ domain.StrategyActions }
type unusedStrategyExecution struct{ domain.StrategyExecution }
type unusedStrategyMarket struct{ domain.MarketReader }

func TestStrategyStandbyDoesNotReadOrTrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "standby")
	if err != nil {
		t.Fatal(err)
	}
	leases := &heldStrategyLease{}
	controller, err := NewStrategyController(StrategyDependencies{
		Leases:      leases,
		Lifecycle:   &unusedStrategyLifecycle{},
		Environment: &unusedStrategyEnvironment{},
		Actions:     &unusedStrategyActions{},
		Execution:   &unusedStrategyExecution{},
		Market:      &unusedStrategyMarket{},
	}, StrategySettings{
		StrategyID: 1,
		OwnerID:    owner,
		LeaseTTL:   3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Every other fake intentionally panics if called. A standby owns no input
	// processing, observation, lifecycle write or trade dispatch.
	worked, err := controller.Tick(ctx)
	if err != nil || worked || leases.calls != 1 {
		t.Fatalf("standby did work or requested an immediate retry: %t, %v", worked, err)
	}
}
