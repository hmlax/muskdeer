package business

import (
	"context"
	"slices"
	"time"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/delivery/strategygrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
	"github.com/hmlax/muskdeer/internal/migrate"
	"github.com/hmlax/muskdeer/internal/outbox"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

const (
	workerExecutionOutbox = "execution.outbox"
	workerPortfolioFills  = "portfolio.fills"
)

// Runtime owns only assembled values. Deer owns every connection and task.
type Runtime struct {
	execution *executiongrpc.Server
	market    *marketgrpc.Server
	strategy  *strategygrpc.Server
	workers   []deer.Worker
}

// New assembles explicit L1 mode and only reads the exact migration ledger.
// Construction starts no task and performs no migration, seeding or grants.
func New(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Runtime, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	if owner == nil || !cfg.GetBusiness().GetEnabled() {
		return nil, ErrInvalidConfig
	}
	db, err := owner.DB()
	if err != nil {
		return nil, err
	}
	if err := migrate.CheckCurrent(ctx, db); err != nil {
		return nil, err
	}
	incarnation, err := ownerid.New(ctx, cfg.InstanceId)
	if err != nil {
		return nil, err
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceMarket {
		return newMarket(ctx, db, incarnation, cfg.Business)
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceStrategy {
		return newStrategy(ctx, db, owner, incarnation, cfg.Business)
	}
	events, err := owner.JetStream()
	if err != nil {
		return nil, err
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceExecution {
		return newExecution(ctx, db, events, incarnation, cfg.Business)
	}
	return newPortfolio(ctx, db, owner, events, cfg.Business)
}

// Workers returns a private plan copy for the Deer lifecycle.
func (r *Runtime) Workers() []deer.Worker { return slices.Clone(r.workers) }

// RegisterGRPC registers the explicitly assembled business capabilities.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	if r.execution != nil {
		executionv1.RegisterExecutionServer(registrar, r.execution)
	}
	if r.market != nil {
		marketv1.RegisterMarketDataServer(registrar, r.market)
	}
	if r.strategy != nil {
		strategyv1.RegisterStrategyManagementServer(registrar, r.strategy)
	}
	return nil
}

func newExecution(ctx context.Context, db *gorm.DB, events *jetstream.Client, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	authority, err := usecase.NewExecutionOwner(leases, incarnation, executionLeaseTTL(cfg))
	if err != nil {
		return nil, err
	}
	handler, err := executionHandler(db, authority)
	if err != nil {
		return nil, err
	}
	store, err := repository.NewOutbox(db)
	if err != nil {
		return nil, err
	}
	publisher, err := outbox.NewPublisher(events)
	if err != nil {
		return nil, err
	}
	worker, err := outbox.New(store, publisher, incarnation)
	if err != nil {
		return nil, err
	}
	additional, err := newRiskWorkers(ctx, db, authority, cfg)
	if err != nil {
		return nil, err
	}
	scenarios, err := newSimulatorWorkers(ctx, db, authority, cfg)
	if err != nil {
		return nil, err
	}
	poll, timeout := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration()
	workers := []deer.Worker{
		{
			Name: workerExecutionOutbox,
			Run:  func(ctx context.Context) error { return runLoop(ctx, poll, timeout, worker.RunOne) },
		},
	}
	workers = append(workers, additional...)
	workers = append(workers, scenarios...)
	return &Runtime{
		execution: handler,
		workers:   workers,
	}, nil
}

func executionHandler(db *gorm.DB, authority *usecase.ExecutionOwner) (*executiongrpc.Server, error) {
	core, err := repository.NewSimulator(db)
	if err != nil {
		return nil, err
	}
	intents, err := repository.NewSimulatorIntents(db)
	if err != nil {
		return nil, err
	}
	orders, err := usecase.NewIntentService(intents, core)
	if err != nil {
		return nil, err
	}
	requests, err := repository.NewSimulatorCancellations(db)
	if err != nil {
		return nil, err
	}
	cancellations, err := usecase.NewCancellationService(requests, intents, core)
	if err != nil {
		return nil, err
	}
	queries, err := repository.NewExecutionQueries(db)
	if err != nil {
		return nil, err
	}
	return executiongrpc.New(executiongrpc.Dependencies{
		Intents:       orders,
		IntentStore:   intents,
		Cancellations: cancellations,
		Queries:       queries,
		Authority:     authority,
	})
}

func newPortfolio(ctx context.Context, db *gorm.DB, owner *deer.Deer, events *jetstream.Client, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		return nil, err
	}
	factory, err := owner.GRPCClients()
	if err != nil {
		return nil, err
	}
	recovery, err := executiongrpc.NewRecovery(factory)
	if err != nil {
		return nil, err
	}
	handler, err := usecase.NewFillConsumer(portfolio, recovery)
	if err != nil {
		return nil, err
	}
	consumer, err := events.OpenConsumer(ctx, cfg.PortfolioConsumer, outbox.ExecutionSubject)
	if err != nil {
		return nil, err
	}
	poll, timeout := cfg.PollInterval.AsDuration(), cfg.OperationTimeout.AsDuration()
	return &Runtime{workers: []deer.Worker{
		{
			Name: workerPortfolioFills,
			Run: func(ctx context.Context) error {
				return runLoop(ctx, poll, timeout, func(ctx context.Context) (bool, error) {
					return consumeFill(ctx, consumer, handler)
				})
			},
			Check: consumer.Check,
		},
	}}, nil
}

func consumeFill(ctx context.Context, consumer *jetstream.Consumer, handler *usecase.FillConsumer) (bool, error) {
	var event commonv1.ExecutionEvent
	return consumer.ConsumeOne(ctx, &event, func(work context.Context, info jetstream.DeliveryInfo) error {
		if event.EventId != info.EventID {
			return eventwire.ErrInvalidEvent
		}
		if err := eventwire.Validate(work, &event); err != nil {
			return err
		}
		if event.EventType != domain.EventTradeFilled {
			return nil
		}
		notice, err := eventwire.Fill(work, &event)
		if err != nil {
			return err
		}
		return handler.Apply(work, notice)
	})
}

func runLoop(ctx context.Context, poll, timeout time.Duration, operation func(context.Context) (bool, error)) error {
	return runOwnedLoop(ctx, poll, func(work context.Context) (bool, error) {
		attempt, cancel := context.WithTimeout(work, timeout)
		defer cancel()
		return operation(attempt)
	})
}

// runOwnedLoop propagates the lifecycle context; operations own their native
// budgets. Batches must bound each constituent operation independently.
func runOwnedLoop(ctx context.Context, poll time.Duration, operation func(context.Context) (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := operation(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
