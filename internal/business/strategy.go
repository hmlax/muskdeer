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
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/delivery/strategygrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

const (
	strategyWorkerPrefix     = "strategy.account."
	strategyOperationsWorker = "strategy.operations"
)

func validateStrategyConfig(cfg *configv1.ServiceConfig) error {
	strategy := cfg.Business.Strategy
	if strategy == nil ||
		cfg.Business.PortfolioConsumer != "" || cfg.Business.Market != nil || cfg.Business.Risk != nil || cfg.Business.ExecutionLeaseTtl != nil || cfg.Business.Simulator != nil {
		return ErrInvalidConfig
	}
	if strategy.ManagementEnabled && !slices.Contains(cfg.GrpcSecurity.AllowedPeerServices, rpcsecurity.ServiceAPI) {
		return ErrInvalidConfig
	}
	if strategy.ControlEnabled && !strategy.ManagementEnabled {
		return ErrInvalidConfig
	}
	if len(strategy.AccountIds) == 0 {
		if !strategy.ManagementEnabled || strategy.LeaseTtl != nil || cfg.GrpcClients != nil {
			return ErrInvalidConfig
		}
		return nil
	}
	if !runtimeAccountsValid(strategy.AccountIds) || cfg.GrpcClients == nil ||
		!boundedDuration(strategy.LeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || strategy.LeaseTtl.AsDuration() <= 2*cfg.Business.OperationTimeout.AsDuration() ||
		!slices.Contains(cfg.GrpcClients.AllowedServices, rpcsecurity.ServiceMarket) || !slices.Contains(cfg.GrpcClients.AllowedServices, rpcsecurity.ServiceExecution) {
		return ErrInvalidConfig
	}
	return nil
}

func newStrategy(ctx context.Context, db *gorm.DB, owner *deer.Deer, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	runtime := &Runtime{}
	if cfg.Strategy.ManagementEnabled {
		queries, err := repository.NewAdminStrategies(db)
		if err != nil {
			return nil, err
		}
		runtime.strategy, err = strategygrpc.New(queries)
		if err != nil {
			return nil, err
		}
		catalog, err := repository.NewAdminCatalog(db)
		if err != nil {
			return nil, err
		}
		runtime.strategyCatalog, err = strategygrpc.NewCatalog(catalog)
		if err != nil {
			return nil, err
		}
		audits, err := repository.NewAdminAudits(db)
		if err != nil {
			return nil, err
		}
		runtime.strategyAudit, err = strategygrpc.NewAudit(audits)
		if err != nil {
			return nil, err
		}
		if err := assembleStrategyRisk(db, cfg.Strategy.ControlEnabled, runtime); err != nil {
			return nil, err
		}
	}
	if cfg.Strategy.ControlEnabled {
		if err := assembleStrategyControl(db, cfg, runtime); err != nil {
			return nil, err
		}
	}
	accounts := slices.Clone(cfg.Strategy.AccountIds)
	if len(accounts) == 0 {
		return runtime, nil
	}
	for _, entity := range []any{&modelv1.SimulatorAccount{}, &modelv1.AccountBudget{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Where("account_id IN ? AND delete_at IS NULL", accounts).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(accounts)) {
			return nil, ErrInvalidConfig
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
	candidates, err := repository.NewStrategyCandidates(db)
	if err != nil {
		return nil, err
	}
	deps := usecase.StrategyDependencies{
		Leases:      leases,
		Lifecycle:   lifecycle,
		Environment: environment,
		Actions:     actions,
		Execution:   execution,
		Market:      market,
	}
	poll, timeout, ttl := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration(), cfg.Strategy.LeaseTtl.AsDuration()
	workers := make([]deer.Worker, 0, len(accounts))
	for index, account := range accounts {
		workers = append(workers, deer.Worker{
			Name: strategyWorkerPrefix + strconv.Itoa(index),
			Run: func(ctx context.Context) error {
				controllers := make(map[int64]*usecase.StrategyController)
				return runOwnedLoop(ctx, poll, func(work context.Context) (bool, error) {
					return strategyAccountTick(work, timeout, account, candidates, controllers, deps, usecase.StrategySettings{
						OwnerID:  incarnation,
						LeaseTTL: ttl,
					})
				})
			},
		})
	}
	runtime.workers = append(runtime.workers, workers...)
	return runtime, nil
}

func assembleStrategyRisk(db *gorm.DB, controlEnabled bool, runtime *Runtime) error {
	risks, err := repository.NewAdminStrategyRisk(db)
	if err != nil {
		return err
	}
	runtime.strategyRiskQuery, err = strategygrpc.NewRiskQuery(risks)
	if err != nil || !controlEnabled {
		return err
	}
	runtime.strategyRiskControl, err = strategygrpc.NewRiskControl(risks)
	return err
}

func assembleStrategyControl(db *gorm.DB, cfg *configv1.BusinessRuntime, runtime *Runtime) error {
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		return err
	}
	runtime.strategyPublication, err = strategygrpc.NewPublication(publisher)
	if err != nil {
		return err
	}
	creator, err := repository.NewAdminStrategyCreator(db)
	if err != nil {
		return err
	}
	runtime.strategyCreation, err = strategygrpc.NewCreation(creator)
	if err != nil {
		return err
	}
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		return err
	}
	runtime.strategyControl, err = strategygrpc.NewControl(controls)
	if err != nil {
		return err
	}
	observer, err := repository.NewAdminOperationObserver(db)
	if err != nil {
		return err
	}
	projector, err := usecase.NewAdminOperationProjector(observer, cfg.OperationTimeout.AsDuration())
	if err != nil {
		return err
	}
	poll := cfg.PollInterval.AsDuration()
	runtime.workers = append(runtime.workers, deer.Worker{
		Name: strategyOperationsWorker,
		Run: func(ctx context.Context) error {
			return runOwnedLoop(ctx, poll, projector.Tick)
		},
	})
	return nil
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
