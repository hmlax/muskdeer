package business

import (
	"context"
	"slices"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	marketv1 "github.com/hmlax/muskdeer/deer/proto/rpc/market/v1"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketworker"
	"github.com/hmlax/muskdeer/internal/repository"
)

const marketWorkerPrefix = "market.feed."

// Market contains only the market-data RPC and feed workers.
type Market struct {
	handler *marketgrpc.Server
	workers []deer.Worker
}

// NewMarket assembles the market service without starting tasks.
func NewMarket(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Market, error) {
	if cfg.GetName() != "market" {
		return nil, ErrInvalidConfig
	}
	db, incarnation, err := runtimeData(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	return assembleMarket(ctx, db, incarnation, cfg.Business)
}

// Workers returns a private market work plan.
func (s *Market) Workers() []deer.Worker { return slices.Clone(s.workers) }

// RegisterGRPC registers only the market-data contract.
func (s *Market) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	marketv1.RegisterMarketDataServer(registrar, s.handler)
	return nil
}

func newMarket(ctx context.Context, db *gorm.DB, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	service, err := assembleMarket(ctx, db, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		market:  service.handler,
		workers: service.Workers(),
	}, nil
}

func assembleMarket(ctx context.Context, db *gorm.DB, incarnation string, cfg *configv1.BusinessRuntime) (*Market, error) {
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

func marketFactory(feed domain.MarketFeedDefinition, cfg *configv1.MarketFeedRuntime, timeout time.Duration) (marketworker.Factory, error) {
	switch feed.Mode {
	case domain.MarketModeLive:
		if hasReplay(cfg) {
			return nil, ErrInvalidConfig
		}
		return func(binding domain.MarketFeedDefinition) (marketworker.Stream, error) {
			return marketworker.NewLive(binding, timeout)
		}, nil
	case domain.MarketModeReplay:
		settings, err := replaySettings(cfg, timeout)
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
		return nil, ErrInvalidConfig
	}
}
