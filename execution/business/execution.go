// Package business assembles only the execution service work plan.
package business

import (
	"context"
	"slices"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	executionv1 "github.com/hmlax/muskdeer/deer/pb/execution/v1"
	"github.com/hmlax/muskdeer/deer/runloop"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	himalaya "github.com/hmlax/muskdeer/execution/adapters/himalaya"
	"github.com/hmlax/muskdeer/execution/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/execution/outbox"
	"github.com/hmlax/muskdeer/execution/usecase"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

const workerExecutionOutbox = "execution.outbox"

// Execution contains only the execution RPC and its owned work plan.
type Execution struct {
	handler *executiongrpc.Server
	workers []deer.Worker
	native  *venueexecution.Runtime
}

// NewExecution assembles the execution service without starting tasks.
func NewExecution(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Execution, error) {
	if cfg.GetName() != "execution" {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	db, incarnation, err := runtimeconfig.Data(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	if cfg.Business.ExecutionVenue == native.Venue {
		factory, err := owner.GRPCClients()
		if err != nil {
			return nil, err
		}
		runtime, err := venueexecution.NewRuntime(ctx, db, incarnation, cfg.Business, factory, owner.Logger(), himalaya.Factory{})
		if err != nil {
			return nil, err
		}
		return &Execution{
			native:  runtime,
			workers: runtime.Workers(),
		}, nil
	}
	events, err := owner.JetStream()
	if err != nil {
		return nil, err
	}
	return Assemble(ctx, db, events, incarnation, cfg.Business)
}

// Workers returns a private execution work plan.
func (s *Execution) Workers() []deer.Worker { return slices.Clone(s.workers) }

// RegisterGRPC registers only the execution contract.
func (s *Execution) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	if s.native != nil {
		s.native.RegisterGRPC(registrar)
		return nil
	}
	executionv1.RegisterExecutionServer(registrar, s.handler)
	return nil
}

// Assemble borrows validated dependencies and constructs work without starting tasks.
func Assemble(ctx context.Context, db *gorm.DB, events *jetstream.Client, incarnation string, cfg *configv1.BusinessRuntime) (*Execution, error) {
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
			Run:  func(ctx context.Context) error { return runloop.Run(ctx, poll, timeout, worker.RunOne) },
		},
	}
	workers = append(workers, additional...)
	workers = append(workers, scenarios...)
	return &Execution{
		handler: handler,
		workers: workers,
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
