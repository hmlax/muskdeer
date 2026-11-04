package testbusiness

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/runloop"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	strategybusiness "github.com/hmlax/muskdeer/strategy/business"
	"github.com/hmlax/muskdeer/strategy/delivery/strategygrpc"
	"github.com/hmlax/muskdeer/strategy/usecase"
)

const strategyOperationsWorker = "strategy.operations"

func newStrategy(ctx context.Context, db *gorm.DB, owner *deer.Deer, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	runtime := &Runtime{}
	if cfg.Strategy.ManagementEnabled {
		if err := assembleStrategyManagement(db, cfg.Strategy.ControlEnabled, runtime); err != nil {
			return nil, err
		}
	}
	if cfg.Strategy.ControlEnabled {
		if err := assembleStrategyControl(db, cfg, runtime); err != nil {
			return nil, err
		}
	}
	workers, err := strategybusiness.AssembleWorkers(ctx, db, owner, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	runtime.workers = append(runtime.workers, workers...)
	return runtime, nil
}

func assembleStrategyManagement(db *gorm.DB, controlEnabled bool, runtime *Runtime) error {
	queries, err := repository.NewAdminStrategies(db)
	if err != nil {
		return err
	}
	runtime.strategy, err = strategygrpc.New(queries)
	if err != nil {
		return err
	}
	catalog, err := repository.NewAdminCatalog(db)
	if err != nil {
		return err
	}
	runtime.strategyCatalog, err = strategygrpc.NewCatalog(catalog)
	if err != nil {
		return err
	}
	audits, err := repository.NewAdminAudits(db)
	if err != nil {
		return err
	}
	runtime.strategyAudit, err = strategygrpc.NewAudit(audits)
	if err != nil {
		return err
	}
	if err := assembleStrategyRisk(db, controlEnabled, runtime); err != nil {
		return err
	}
	if err := assembleStrategyCancellations(db, controlEnabled, runtime); err != nil {
		return err
	}
	if err := assembleStrategyEmergencies(db, controlEnabled, runtime); err != nil {
		return err
	}
	if err := assembleStrategyOrderQueries(db, runtime); err != nil {
		return err
	}
	if err := assembleStrategyMarkets(db, runtime); err != nil {
		return err
	}
	if err := assembleStrategyPortfolios(db, runtime); err != nil {
		return err
	}
	if err := assembleStrategyRiskEvents(db, runtime); err != nil {
		return err
	}
	return assembleStrategyOverview(db, runtime)
}

func assembleStrategyOverview(db *gorm.DB, runtime *Runtime) error {
	overview, err := repository.NewAdminOverview(db)
	if err != nil {
		return err
	}
	runtime.strategyOverview, err = strategygrpc.NewOverviewQuery(overview)
	return err
}

func assembleStrategyPortfolios(db *gorm.DB, runtime *Runtime) error {
	portfolios, err := repository.NewAdminPortfolios(db)
	if err != nil {
		return err
	}
	runtime.strategyPortfolios, err = strategygrpc.NewPortfolioQuery(portfolios)
	return err
}

func assembleStrategyRiskEvents(db *gorm.DB, runtime *Runtime) error {
	events, err := repository.NewAdminRiskEvents(db)
	if err != nil {
		return err
	}
	runtime.strategyRiskEvents, err = strategygrpc.NewRiskEventQuery(events)
	return err
}

func assembleStrategyOrderQueries(db *gorm.DB, runtime *Runtime) error {
	orders, err := repository.NewAdminOrders(db)
	if err != nil {
		return err
	}
	runtime.strategyOrders, err = strategygrpc.NewOrderQuery(orders)
	if err != nil {
		return err
	}
	trades, err := repository.NewAdminTrades(db)
	if err != nil {
		return err
	}
	runtime.strategyTrades, err = strategygrpc.NewTradeQuery(trades)
	return err
}

func assembleStrategyMarkets(db *gorm.DB, runtime *Runtime) error {
	markets, err := repository.NewAdminMarkets(db)
	if err != nil {
		return err
	}
	runtime.strategyMarkets, err = strategygrpc.NewMarketQuery(markets)
	return err
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

func assembleStrategyCancellations(db *gorm.DB, controlEnabled bool, runtime *Runtime) error {
	requests, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		return err
	}
	runtime.strategyCancellationQuery, err = strategygrpc.NewCancellationQuery(requests)
	if err != nil || !controlEnabled {
		return err
	}
	runtime.strategyCancellationControl, err = strategygrpc.NewCancellationControl(requests)
	return err
}

func assembleStrategyEmergencies(db *gorm.DB, controlEnabled bool, runtime *Runtime) error {
	requests, err := repository.NewAdminEmergencyCancellations(db)
	if err != nil {
		return err
	}
	runtime.strategyEmergencyQuery, err = strategygrpc.NewEmergencyQuery(requests)
	if err != nil || !controlEnabled {
		return err
	}
	runtime.strategyEmergencyControl, err = strategygrpc.NewEmergencyControl(requests)
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
			return runloop.RunOwned(ctx, poll, projector.Tick)
		},
	})
	return nil
}
