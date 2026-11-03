package business

import (
	"context"
	"slices"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/eventwire"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/usecase"
	"github.com/hmlax/muskdeer/execution/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/execution/outbox"
)

const workerPortfolioFills = "portfolio.fills"

// Portfolio owns the fill-consumer work plan and no management RPCs.
type Portfolio struct{ workers []deer.Worker }

// NewPortfolio assembles the projection service without starting tasks.
func NewPortfolio(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Portfolio, error) {
	if cfg.GetName() != "portfolio" {
		return nil, ErrInvalidConfig
	}
	db, _, err := runtimeData(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	events, err := owner.JetStream()
	if err != nil {
		return nil, err
	}
	return assemblePortfolio(ctx, db, owner, events, cfg.Business)
}

// Workers returns a private portfolio work plan.
func (s *Portfolio) Workers() []deer.Worker { return slices.Clone(s.workers) }

func newPortfolio(ctx context.Context, db *gorm.DB, owner *deer.Deer, events *jetstream.Client, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	service, err := assemblePortfolio(ctx, db, owner, events, cfg)
	if err != nil {
		return nil, err
	}
	return &Runtime{workers: service.Workers()}, nil
}

func assemblePortfolio(ctx context.Context, db *gorm.DB, owner *deer.Deer, events *jetstream.Client, cfg *configv1.BusinessRuntime) (*Portfolio, error) {
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
