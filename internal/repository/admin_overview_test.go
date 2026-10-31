package repository

import (
	"errors"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminOverviewCountsKeepRequestedTargetsSeparateAndRejectOverflow(t *testing.T) {
	rows := []adminOverviewCountRow{
		{
			State: string(domain.StrategyStateCreated),
			Count: 9007199254740993,
		},
		{
			State: string(domain.StrategyStateRunning),
			Count: 2,
		},
		{
			State:          string(domain.StrategyStateRunning),
			DesiredRunning: true,
			Count:          3,
		},
	}
	value, err := adminOverviewCounts(rows)
	if err != nil || value.Total != 9007199254740998 || value.RequestedRunning != 3 || len(value.StoredStates) != 2 || value.StoredStates[1].State != domain.StrategyStateRunning || value.StoredStates[1].Count != 5 {
		t.Fatal("large counts were rounded or desired target became stored runtime", err)
	}
	for _, invalid := range [][]adminOverviewCountRow{
		{
			{
				State: "HEALTHY",
				Count: 1,
			},
		},
		{
			{
				State: string(domain.StrategyStateRunning),
				Count: 0,
			},
		},
		{
			{
				State: string(domain.StrategyStateRunning),
				Count: -1,
			},
		},
		{rows[1], rows[0]},
		{rows[1], rows[1]},
		{rows[2], rows[1]},
		{
			{
				State: string(domain.StrategyStateCreated),
				Count: math.MaxInt64,
			},
			rows[1],
		},
		make([]adminOverviewCountRow, maximumOverviewStateGroups+1),
	} {
		if value, err := adminOverviewCounts(invalid); !errors.Is(err, domain.ErrAdminUnavailable) || value.Total != 0 || value.StoredStates != nil {
			t.Fatal("invalid counts escaped as partial success", err)
		}
	}
	if _, err := NewAdminOverview(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil overview database accepted", err)
	}
}

func TestAdminOverviewLeaseValuesRetainZeroSlotsAndReleasedLegacyHolders(t *testing.T) {
	strategy := &modelv1.StrategyInstance{
		Id:                9007199254740993,
		RuntimeOwnerId:    "legacy-holder",
		RuntimeOwnerEpoch: 9007199254740994,
	}
	row := &modelv1.StrategyLease{
		Id:                 1,
		StrategyInstanceId: strategy.Id,
		OwnerId:            strategy.RuntimeOwnerId,
		Epoch:              strategy.RuntimeOwnerEpoch,
		ExpiresAt:          2000,
	}
	value, err := adminOverviewLeaseValue(row, strategy, 1000)
	if err != nil || !value.OwnerRecorded || !value.RuntimeMatchesLease || !value.UnexpiredAtObservation || value.Epoch != 9007199254740994 {
		t.Fatal("legacy recorded identity or high epoch was reinterpreted as authorization syntax", err)
	}
	row.ExpiresAt = 0
	released, err := adminOverviewLeaseValue(row, strategy, 1000)
	if err != nil || !released.OwnerRecorded || !released.RuntimeMatchesLease || released.UnexpiredAtObservation || released.ExpiresAt.UnixMilli() != 0 {
		t.Fatal("release forgot retained identity or claimed a live lease", err)
	}
	row.Epoch, row.OwnerId = 0, ""
	unclaimed, err := adminOverviewLeaseValue(row, strategy, 1000)
	if err != nil || unclaimed.OwnerRecorded || unclaimed.UnexpiredAtObservation || unclaimed.RuntimeMatchesLease || unclaimed.Epoch != 0 {
		t.Fatal("zero-epoch persisted slot became an owner", err)
	}
	for _, change := range []func(*modelv1.StrategyLease){
		func(r *modelv1.StrategyLease) { r.Id = 0 },
		func(r *modelv1.StrategyLease) { r.StrategyInstanceId++ },
		func(r *modelv1.StrategyLease) { r.Epoch = -1 },
		func(r *modelv1.StrategyLease) { r.ExpiresAt = 1 },
		func(r *modelv1.StrategyLease) { r.OwnerId = "unexpected-zero-epoch-owner" },
		func(r *modelv1.StrategyLease) { r.Epoch = 1 },
	} {
		candidate := proto.Clone(row).(*modelv1.StrategyLease)
		change(candidate)
		if value, err := adminOverviewLeaseValue(candidate, strategy, 1000); !errors.Is(err, domain.ErrAdminUnavailable) || value != nil {
			t.Fatal("invalid retained lease escaped read validation", err)
		}
	}
}
