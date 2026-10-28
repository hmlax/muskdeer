package business

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestStrategyAccountDoesNotPassSpentBudgetToAnotherInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "spent-account-cycle")
	if err != nil {
		t.Fatal(err)
	}
	leases := &cancelingStrategyLease{cancel: cancel}
	unused := &cycleUnusedStrategyPorts{}
	controllers := map[int64]*usecase.StrategyController{99: nil}
	worked, err := strategyAccountTick(ctx, time.Second, "account", &cycleStrategyCandidates{}, controllers, usecase.StrategyDependencies{
		Leases:      leases,
		Lifecycle:   unused,
		Environment: unused,
		Actions:     unused,
		Execution:   unused,
		Market:      unused,
	}, usecase.StrategySettings{
		OwnerID:  owner,
		LeaseTTL: 3 * time.Second,
	})
	if err != nil || worked || leases.calls != 1 || len(controllers) != 1 {
		t.Fatal("spent successful tick budget was passed to a second instance")
	}
}

func TestStrategyAccountGivesEachInstanceItsOwnNativeBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		owner, err := ownerid.New(ctx, "independent-instance-budget")
		if err != nil {
			t.Fatal(err)
		}
		leases := &delayedStrategyLease{}
		unused := &cycleUnusedStrategyPorts{}
		_, err = strategyAccountTick(ctx, time.Second, "account", &cycleStrategyCandidates{}, make(map[int64]*usecase.StrategyController), usecase.StrategyDependencies{
			Leases:      leases,
			Lifecycle:   unused,
			Environment: unused,
			Actions:     unused,
			Execution:   unused,
			Market:      unused,
		}, usecase.StrategySettings{
			OwnerID:  owner,
			LeaseTTL: 3 * time.Second,
		})
		if err != nil || leases.calls != 2 || ctx.Err() != nil {
			t.Fatalf("two 600ms instance operations shared one 1s budget: calls=%d error=%v parent=%v", leases.calls, err, ctx.Err())
		}
	})
}

type delayedStrategyLease struct {
	domain.LeaseRepository
	calls int
}

func (l *delayedStrategyLease) Acquire(ctx context.Context, _ domain.LeaseKey, _ string, _ time.Duration) (domain.LeaseToken, error) {
	l.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) != time.Second {
		return domain.LeaseToken{}, errors.New("instance did not receive an independent bounded deadline")
	}
	time.Sleep(600 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		return domain.LeaseToken{}, err
	}
	return domain.LeaseToken{}, domain.ErrLeaseHeld
}

type cancelingStrategyLease struct {
	domain.LeaseRepository
	cancel context.CancelFunc
	calls  int
}

func (l *cancelingStrategyLease) Acquire(context.Context, domain.LeaseKey, string, time.Duration) (domain.LeaseToken, error) {
	l.calls++
	l.cancel()
	return domain.LeaseToken{}, domain.ErrLeaseHeld
}

type cycleStrategyCandidates struct{}

func (*cycleStrategyCandidates) ForAccount(context.Context, string) ([]int64, error) {
	return []int64{1, 2}, nil
}

type cycleUnusedStrategyPorts struct {
	domain.StrategyLifecycle
	domain.StrategyEnvironment
	domain.StrategyActions
	domain.StrategyExecution
	domain.MarketReader
}
