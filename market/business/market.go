// Package business assembles only the market service work plan.
package business

import (
	"context"
	"slices"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	marketconfigv1 "github.com/hmlax/muskdeer/deer/proto/market/config/v1"
	marketv1 "github.com/hmlax/muskdeer/deer/proto/market/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/market/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/market/marketworker"
	"github.com/hmlax/muskdeer/market/usdm"
)

const marketWorkerPrefix = "market.feed."

// Market contains only the market-data RPC and feed workers.
type Market struct {
	handler *marketgrpc.Server
	workers []deer.Worker
	native  *usdm.Runtime
}

// NewMarket assembles the market service without starting tasks.
func NewMarket(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Market, error) {
	if cfg.GetName() != "market" {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	db, incarnation, err := runtimeconfig.Data(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	if cfg.Business.ExecutionVenue == native.Venue {
		runtime, err := usdm.NewRuntime(ctx, db, incarnation, cfg.Business, owner.Logger())
		if err != nil {
			return nil, err
		}
		return &Market{
			native:  runtime,
			workers: runtime.Workers(),
		}, nil
	}
	return Assemble(ctx, db, incarnation, cfg.Business)
}

// Workers returns a private market work plan.
func (s *Market) Workers() []deer.Worker { return slices.Clone(s.workers) }

// RegisterGRPC registers only the market-data contract.
func (s *Market) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	if s.native != nil {
		s.native.RegisterGRPC(registrar)
		return nil
	}
	marketv1.RegisterMarketDataServer(registrar, s.handler)
	return nil
}

// Assemble borrows validated dependencies and constructs work without starting tasks.
func Assemble(ctx context.Context, db *gorm.DB, incarnation string, cfg *configv1.BusinessRuntime) (*Market, error) {
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		return nil, err
	}
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	handler, err := marketgrpc.New(marketgrpc.Dependencies{
		Feeds:     feeds,
		Snapshots: snapshots,
	})
	if err != nil {
		return nil, err
	}
	workers := make([]deer.Worker, 0, len(cfg.Market.Feeds))
	for _, requested := range cfg.Market.Feeds {
		feed, err := feeds.Get(ctx, requested.FeedId)
		if err != nil {
			return nil, err
		}
		factory, err := marketFactory(feed, requested, cfg.OperationTimeout.AsDuration())
		if err != nil {
			return nil, err
		}
		worker, err := marketworker.New(marketworker.Config{
			Feed:             feed,
			OwnerID:          incarnation,
			LeaseTTL:         cfg.Market.LeaseTtl.AsDuration(),
			OperationTimeout: cfg.OperationTimeout.AsDuration(),
			RetryInterval:    cfg.PollInterval.AsDuration(),
		}, marketworker.Dependencies{
			Leases:    leases,
			Snapshots: snapshots,
			Factory:   factory,
		})
		if err != nil {
			return nil, err
		}
		workers = append(workers, deer.Worker{
			Name: marketWorkerPrefix + strconv.FormatInt(feed.ID, 10),
			Run:  worker.Run,
		})
	}
	return &Market{
		handler: handler,
		workers: workers,
	}, nil
}

func marketFactory(feed domain.MarketFeedDefinition, cfg *marketconfigv1.MarketFeedRuntime, timeout time.Duration) (marketworker.Factory, error) {
	switch feed.Mode {
	case domain.MarketModeLive:
		if runtimeconfig.HasReplay(cfg) {
			return nil, runtimeconfig.ErrInvalidConfig
		}
		return func(binding domain.MarketFeedDefinition) (marketworker.Stream, error) {
			return marketworker.NewLive(binding, timeout)
		}, nil
	case domain.MarketModeReplay:
		settings, err := runtimeconfig.ReplaySettings(cfg, timeout)
		if err != nil {
			return nil, err
		}
		if _, err := marketworker.NewReplay(feed, settings); err != nil {
			return nil, err
		}
		return func(binding domain.MarketFeedDefinition) (marketworker.Stream, error) {
			return marketworker.NewReplay(binding, settings)
		}, nil
	default:
		return nil, runtimeconfig.ErrInvalidConfig
	}
}
