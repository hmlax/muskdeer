package business

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

const riskWorkerPrefix = "execution.risk."

type riskExecutionOwner interface {
	ForAccount(context.Context, string) (domain.LeaseToken, error)
}

func validateRiskConfig(cfg *configv1.BusinessRuntime) error {
	if cfg.ExecutionLeaseTtl != nil && (!boundedDuration(cfg.ExecutionLeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || cfg.ExecutionLeaseTtl.AsDuration() <= 2*cfg.OperationTimeout.AsDuration()) {
		return ErrInvalidConfig
	}
	if cfg.Risk == nil {
		return nil
	}
	if cfg.ExecutionLeaseTtl == nil || !runtimeAccountsValid(cfg.Risk.AccountIds) {
		return ErrInvalidConfig
	}
	return nil
}

func executionLeaseTTL(cfg *configv1.BusinessRuntime) time.Duration {
	if cfg.ExecutionLeaseTtl == nil {
		return domain.MaxLeaseTTL
	}
	return cfg.ExecutionLeaseTtl.AsDuration()
}

func newRiskWorkers(ctx context.Context, db *gorm.DB, owner riskExecutionOwner, cfg *configv1.BusinessRuntime) ([]deer.Worker, error) {
	if cfg.Risk == nil {
		return nil, nil
	}
	accounts := slices.Clone(cfg.Risk.AccountIds)
	for _, entity := range []any{&modelv1.AccountBudget{}, &modelv1.SimulatorAccount{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Where("account_id IN ? AND delete_at IS NULL", accounts).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(accounts)) {
			return nil, ErrInvalidConfig
		}
	}
	candidates, err := repository.NewRiskCandidates(db)
	if err != nil {
		return nil, err
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		return nil, err
	}
	poll, timeout := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration()
	workers := make([]deer.Worker, 0, len(accounts))
	for index, account := range accounts {
		workers = append(workers, deer.Worker{
			Name: riskWorkerPrefix + strconv.Itoa(index),
			Run: func(ctx context.Context) error {
				return runLoop(ctx, poll, timeout, func(ctx context.Context) (bool, error) {
					return riskEvaluationTick(ctx, account, owner, candidates, evaluator)
				})
			},
		})
	}
	return workers, nil
}

func riskEvaluationTick(ctx context.Context, account string, owner riskExecutionOwner, candidates domain.RiskCandidates, evaluator domain.RiskEvaluator) (bool, error) {
	authority, err := owner.ForAccount(ctx, account)
	if errors.Is(err, domain.ErrLeaseHeld) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if authority.Key.Kind != domain.LeaseKindExecution || authority.Key.AccountID != account || authority.Key.StrategyID != 0 || authority.Key.MarketFeedID != 0 || authority.Epoch <= 0 || !ownerid.Valid(authority.OwnerID) {
		return false, domain.ErrLeaseLost
	}
	ids, err := candidates.DesiredForAccount(ctx, account)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if _, err := evaluator.Evaluate(ctx, id); err != nil {
			return false, err
		}
	}
	// Eligibility is refreshed at the configured cadence, never a busy loop.
	return false, nil
}
