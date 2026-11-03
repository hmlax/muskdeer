// Package business assembles only the portfolio service work plan.
package business

import (
	"context"
	"slices"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/runloop"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/eventwire"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/execution/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/execution/outbox"
	nativeportfolio "github.com/hmlax/muskdeer/portfolio/native"
	"github.com/hmlax/muskdeer/portfolio/usecase"
)

const workerPortfolioFills = "portfolio.fills"

// Portfolio owns the fill-consumer work plan and no management RPCs.
type Portfolio struct {
	workers []deer.Worker
	native  *nativeportfolio.Runtime
}

// NewPortfolio assembles the projection service without starting tasks.
func NewPortfolio(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Portfolio, error) {
	if cfg.GetName() != "portfolio" {
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
		runtime, err := nativeportfolio.NewRuntime(db, incarnation, cfg.Business, factory, owner.Logger())
		if err != nil {
			return nil, err
		}
		return &Portfolio{
			native:  runtime,
			workers: runtime.Workers(),
		}, nil
	}
	events, err := owner.JetStream()
	if err != nil {
		return nil, err
	}
	return Assemble(ctx, db, owner, events, cfg.Business)
}

// Workers returns a private portfolio work plan.
func (s *Portfolio) Workers() []deer.Worker { return slices.Clone(s.workers) }

// RegisterGRPC exposes only native observations when explicitly configured.
func (s *Portfolio) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	if s.native != nil {
		s.native.RegisterGRPC(registrar)
	}
	return nil
}

// Assemble borrows validated dependencies and constructs work without starting tasks.
func Assemble(ctx context.Context, db *gorm.DB, owner *deer.Deer, events *jetstream.Client, cfg *configv1.BusinessRuntime) (*Portfolio, error) {
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
	return &Portfolio{workers: []deer.Worker{
		{
			Name: workerPortfolioFills,
			Run: func(ctx context.Context) error {
				return runloop.Run(ctx, poll, timeout, func(ctx context.Context) (bool, error) {
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
