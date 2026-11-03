// Package business assembles only the strategy account controllers.
package business

import (
	"context"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/runloop"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/execution/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/market/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/strategy/usecase"
)

const strategyWorkerPrefix = "strategy.account."

// Strategy owns only account controllers; it exposes no management RPCs.
type Strategy struct{ workers []deer.Worker }

// NewStrategy assembles independent strategy workers without management delivery.
func NewStrategy(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Strategy, error) {
	if cfg.GetName() != "strategy" || cfg.GetBusiness().GetStrategy().GetManagementEnabled() || cfg.GetBusiness().GetStrategy().GetControlEnabled() {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	db, incarnation, err := runtimeconfig.Data(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	workers, err := AssembleWorkers(ctx, db, owner, incarnation, cfg.Business)
	if err != nil {
		return nil, err
	}
	return &Strategy{workers: workers}, nil
}

// Workers returns a private strategy work plan.
func (s *Strategy) Workers() []deer.Worker { return slices.Clone(s.workers) }

// AssembleWorkers borrows validated dependencies for account controllers without starting tasks.
func AssembleWorkers(ctx context.Context, db *gorm.DB, owner *deer.Deer, incarnation string, cfg *configv1.BusinessRuntime) ([]deer.Worker, error) {
	accounts := slices.Clone(cfg.Strategy.AccountIds)
	if len(accounts) == 0 {
		return nil, nil
	}
	for _, entity := range []any{&modelv1.SimulatorAccount{}, &modelv1.AccountBudget{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Where("account_id IN ? AND delete_at IS NULL", accounts).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(accounts)) {
			return nil, runtimeconfig.ErrInvalidConfig
		}
	}
	factory, err := owner.GRPCClients()
	if err != nil {
		return nil, err
	}
	routing, err := repository.NewExecutionRouting(db)
	if err != nil {
		return nil, err
	}
	execution, err := executiongrpc.NewStrategyClient(factory, routing)
	if err != nil {
		return nil, err
	}
	market, err := marketgrpc.NewReader(factory)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		return nil, err
	}
	environment, err := repository.NewStrategyEnvironment(db)
	if err != nil {
		return nil, err
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		return nil, err
	}
	cancellations, err := repository.NewStrategyOrderCancellations(db)
	if err != nil {
		return nil, err
	}
	emergencies, err := repository.NewStrategyEmergencyCancellations(db)
	if err != nil {
		return nil, err
	}
	candidates, err := repository.NewStrategyCandidates(db)
	if err != nil {
		return nil, err
	}
	deps := usecase.StrategyDependencies{
		Leases:        leases,
		Lifecycle:     lifecycle,
		Environment:   environment,
		Actions:       actions,
		Execution:     execution,
		Market:        market,
		Cancellations: cancellations,
		Emergencies:   emergencies,
	}
	poll, timeout, ttl := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration(), cfg.Strategy.LeaseTtl.AsDuration()
	workers := make([]deer.Worker, 0, len(accounts))
	for index, account := range accounts {
		workers = append(workers, deer.Worker{
			Name: strategyWorkerPrefix + strconv.Itoa(index),
			Run: func(ctx context.Context) error {
				controllers := make(map[int64]*usecase.StrategyController)
				return runloop.RunOwned(ctx, poll, func(work context.Context) (bool, error) {
					return strategyAccountTick(work, timeout, account, candidates, controllers, deps, usecase.StrategySettings{
						OwnerID:  incarnation,
						LeaseTTL: ttl,
					})
				})
			},
		})
	}
	return workers, nil
}

func strategyAccountTick(ctx context.Context, timeout time.Duration, account string, candidates domain.StrategyCandidates, controllers map[int64]*usecase.StrategyController, deps usecase.StrategyDependencies, settings usecase.StrategySettings) (bool, error) {
	if ctx == nil || timeout <= 0 {
		return false, domain.ErrInvalidOrder
	}
	query, cancelQuery := context.WithTimeout(ctx, timeout)
	ids, err := candidates.ForAccount(query, account)
	cancelQuery()
	if err != nil {
		return false, err
	}
	if len(ids) > domain.MaxAccountStrategyInstances {
		return false, domain.ErrRiskDenied
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return false, nil
		}
		if id <= 0 {
			return false, domain.ErrInvalidOrder
		}
		if _, duplicate := seen[id]; duplicate {
			return false, domain.ErrInvalidOrder
		}
		seen[id] = struct{}{}
	}
	for id := range controllers {
		if _, exists := seen[id]; !exists {
			delete(controllers, id)
		}
	}
	for _, id := range ids {
		controller := controllers[id]
		if controller == nil {
			settings.StrategyID = id
			controller, err = usecase.NewStrategyController(deps, settings)
			if err != nil {
				return false, err
			}
			controllers[id] = controller
		}
		call, cancelCall := context.WithTimeout(ctx, timeout)
		_, err := controller.Tick(call)
		cancelCall()
		if err != nil {
			return false, err
		}
		if ctx.Err() != nil {
			// Parent lifecycle cancellation prevents another instance starting.
			return false, nil
		}
	}
	return false, nil
}
