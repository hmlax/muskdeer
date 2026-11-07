package native

import (
	"context"
	"errors"
	"log/slog"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func syntheticWorkers(db *gorm.DB, owner string, cfg *venueconfig.RuntimeConfig, factory *rpcclient.Factory, logger *logging.Logger, leases domain.LeaseRepository) []deer.Worker {
	if !cfg.Venue.GetSyntheticTape().GetEnabled() {
		return nil
	}
	var workers []deer.Worker
	for _, profile := range cfg.Venue.SyntheticTape.Profiles {
		workers = append(workers, deer.Worker{
			Name: values.SyntheticWorker + profile.Symbol,
			Run: func(ctx context.Context) error {
				for sequence := cfg.ResumeSequence; values.SyntheticSlotAvailable(cfg.Venue.SyntheticTape, sequence) && ctx.Err() == nil; {
					op, cancel := context.WithTimeout(ctx, cfg.OperationTimeout)
					err := syntheticTick(op, db, leases, factory, owner, cfg, profile.Symbol, sequence)
					cancel()
					if err == nil {
						sequence++
					} else if !errors.Is(err, errInstancePaused) && ctx.Err() == nil {
						if err := logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("strategy", values.SyntheticWorker), slog.String("symbol", profile.Symbol)); err != nil {
							return err
						}
					}
					if err := values.Wait(ctx, cfg.Venue.SyntheticTape.Interval.AsDuration()); err != nil {
						return err
					}
				}
				<-ctx.Done()
				return ctx.Err()
			},
		})
	}
	return workers
}

var errInstancePaused = errors.New("strategy instance is paused")

func syntheticTick(ctx context.Context, db *gorm.DB, leases domain.LeaseRepository, factory *rpcclient.Factory, owner string, cfg *venueconfig.RuntimeConfig, symbol string, sequence uint64) error {
	active, err := instanceconfig.Active(ctx, db, cfg.InstanceID, cfg.Revision)
	if err != nil {
		return err
	}
	if !active {
		return errInstancePaused
	}
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: values.SyntheticStrategyKey(cfg.Venue.SyntheticTape.RunId, symbol),
	}, owner, cfg.Venue.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	token, err = leases.Renew(ctx, token, cfg.Venue.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	market, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return err
	}
	book, err := marketvenuev1.NewMarketClient(market).GetBook(ctx, &marketvenuev1.BookRequest{Symbol: symbol})
	if err != nil {
		return err
	}
	proposal := &commonv1.SyntheticTapePlan{
		RunId:              cfg.Venue.SyntheticTape.RunId,
		Symbol:             symbol,
		Sequence:           sequence,
		StrategyInstanceId: cfg.InstanceID,
		StrategyRevision:   cfg.Revision,
		Book:               book,
		Strategy:           values.Authority(token),
	}
	if cfg.Venue.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		proposal.SourceTrade, err = marketvenuev1.NewMarketClient(market).GetTrade(ctx, &marketvenuev1.TradeRequest{Symbol: symbol})
		if err != nil {
			return err
		}
	}
	plan, err := values.SyntheticSourcePlan(cfg.Venue, proposal)
	if err != nil {
		return err
	}
	instance, err := repository.NativeExecutionInstance(ctx, db, values.SyntheticExecutionKey(plan.RunId, symbol))
	if err != nil {
		return err
	}
	execution, err := factory.Client(rpcclient.Target{
		Service:    rpcsecurity.ServiceExecution,
		InstanceID: instance,
	})
	if err != nil {
		return err
	}
	result, err := executionvenuev1.NewExecutionClient(execution).PublishSynthetic(ctx, plan)
	if err != nil {
		return err
	}
	if result.GetState() != values.SyntheticPublished && result.GetState() != values.SyntheticUnknown {
		return values.ErrInvalid
	}
	return nil
}
