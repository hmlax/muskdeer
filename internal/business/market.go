package business

import (
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketworker"
	"github.com/hmlax/muskdeer/internal/repository"
)

const marketWorkerPrefix = "market.feed."

func newMarket(ctx context.Context, db *gorm.DB, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
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
	return &Runtime{
		market:  handler,
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
