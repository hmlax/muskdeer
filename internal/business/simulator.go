package business

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

const workerSimulatorCancel = "execution.simulator.cancel"

type simulatorCancelConfirmer interface {
	ConfirmCancel(context.Context, string, string) (domain.CoreOrder, error)
}

func validateSimulatorConfig(cfg *configv1.BusinessRuntime) error {
	if cfg.Simulator == nil {
		return nil
	}
	if cfg.ExecutionLeaseTtl == nil || !runtimeAccountsValid(cfg.Simulator.AccountIds) ||
		!boundedDuration(cfg.Simulator.CancelDelay, time.Millisecond, 5*time.Second) {
		return ErrInvalidConfig
	}
	return nil
}

func newSimulatorWorkers(ctx context.Context, db *gorm.DB, owner riskExecutionOwner, cfg *configv1.BusinessRuntime) ([]deer.Worker, error) {
	if cfg.Simulator == nil {
		return nil, nil
	}
	accounts := slices.Clone(cfg.Simulator.AccountIds)
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).
		Where("account_id IN ? AND delete_at IS NULL", accounts).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != int64(len(accounts)) {
		return nil, ErrInvalidConfig
	}
	candidates, err := repository.NewSimulatorScenarios(db)
	if err != nil {
		return nil, err
	}
	core, err := repository.NewSimulator(db)
	if err != nil {
		return nil, err
	}
	poll, timeout := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration()
	delay := cfg.Simulator.CancelDelay.AsDuration()
	return []deer.Worker{{
		Name: workerSimulatorCancel,
		Run: func(ctx context.Context) error {
			return runLoop(ctx, poll, timeout, func(ctx context.Context) (bool, error) {
				return simulatorCancellationTick(ctx, accounts, delay, owner, candidates, core)
			})
		},
	}}, nil
}

func simulatorCancellationTick(ctx context.Context, accounts []string, delay time.Duration, owner riskExecutionOwner, candidates domain.SimulatorScenarios, core simulatorCancelConfirmer) (bool, error) {
	worked := false
	for _, account := range accounts {
		token, err := owner.ForAccount(ctx, account)
		if errors.Is(err, domain.ErrLeaseHeld) {
			continue
		}
		if err != nil {
			return worked, err
		}
		if token.Key.Kind != domain.LeaseKindExecution || token.Key.AccountID != account || token.Key.StrategyID != 0 || token.Key.MarketFeedID != 0 || token.Epoch <= 0 || !ownerid.Valid(token.OwnerID) {
			return worked, domain.ErrLeaseLost
		}
		client, err := candidates.DueCancellation(ctx, account, delay)
		if err != nil {
			return worked, err
		}
		if client == "" {
			continue
		}
		if _, err := core.ConfirmCancel(ctx, account, client); err != nil {
			return worked, err
		}
		worked = true
	}
	return worked, nil
}
