package native

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	rpc "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func syntheticWorkers(db *gorm.DB, owner string, cfg *configv1.BusinessRuntime, factory *rpcclient.Factory, logger *logging.Logger, leases domain.LeaseRepository) []deer.Worker {
	if !cfg.Himalaya.GetSyntheticTape().GetEnabled() {
		return nil
	}
	var workers []deer.Worker
	for _, profile := range cfg.Himalaya.SyntheticTape.Profiles {
		workers = append(workers, deer.Worker{
			Name: values.SyntheticWorker + profile.Symbol,
			Run: func(ctx context.Context) error {
				for sequence := uint32(0); sequence < cfg.Himalaya.SyntheticTape.MaximumEvents && ctx.Err() == nil; {
					op, cancel := context.WithTimeout(ctx, cfg.OperationTimeout.AsDuration())
					err := syntheticTick(op, db, leases, factory, owner, cfg, profile.Symbol, sequence)
					cancel()
					if err == nil {
						sequence++
					} else if ctx.Err() == nil {
						if err := logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("strategy", values.SyntheticWorker), slog.String("symbol", profile.Symbol)); err != nil {
							return err
						}
					}
					if err := values.Wait(ctx, cfg.Himalaya.SyntheticTape.Interval.AsDuration()); err != nil {
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

func syntheticTick(ctx context.Context, db *gorm.DB, leases domain.LeaseRepository, factory *rpcclient.Factory, owner string, cfg *configv1.BusinessRuntime, symbol string, sequence uint32) error {
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: values.SyntheticStrategyKey(cfg.Himalaya.SyntheticTape.RunId, symbol),
	}, owner, cfg.Himalaya.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	token, err = leases.Renew(ctx, token, cfg.Himalaya.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	market, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return err
	}
	book, err := rpc.NewMarketClient(market).GetBook(ctx, &rpc.BookRequest{Symbol: symbol})
	if err != nil {
		return err
	}
	proposal := &commonv1.SyntheticTapePlan{
		RunId:    cfg.Himalaya.SyntheticTape.RunId,
		Symbol:   symbol,
		Sequence: sequence,
		Book:     book,
		Strategy: values.Authority(token),
	}
	if cfg.Himalaya.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		proposal.SourceTrade, err = rpc.NewMarketClient(market).GetTrade(ctx, &rpc.TradeRequest{Symbol: symbol})
		if err != nil {
			return err
		}
	}
	plan, err := values.SyntheticSourcePlan(cfg.Himalaya, proposal)
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
	result, err := rpc.NewExecutionClient(execution).PublishSynthetic(ctx, plan)
	if err != nil {
		return err
	}
	if result.GetState() != values.SyntheticPublished && result.GetState() != values.SyntheticUnknown {
		return values.ErrInvalid
	}
	return nil
}
