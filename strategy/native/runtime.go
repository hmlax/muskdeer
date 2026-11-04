package native

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

// Workers describes one fenced, bounded maker actor per configured fund.
func Workers(ctx context.Context, db *gorm.DB, owner string, input *configv1.BusinessRuntime, factory *rpcclient.Factory, logger *logging.Logger) ([]deer.Worker, error) {
	cfg, err := values.LoadRuntime(ctx, db, input)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	var workers []deer.Worker
	for _, profile := range cfg.Himalaya.Profiles {
		workers = append(workers, deer.Worker{
			Name: "strategy.native." + strings.ToLower(profile.Symbol),
			Run: func(ctx context.Context) error {
				failed := false
				for ctx.Err() == nil {
					op, cancel := context.WithTimeout(ctx, cfg.OperationTimeout)
					err := tick(op, db, leases, factory, owner, cfg, profile)
					cancel()
					if err != nil && !failed && ctx.Err() == nil {
						if e := logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("symbol", profile.Symbol)); e != nil {
							return e
						}
					}
					failed = err != nil
					if err := values.Wait(ctx, cfg.PollInterval); err != nil {
						return err
					}
				}
				return ctx.Err()
			},
		})
	}
	return append(workers, syntheticWorkers(db, owner, cfg, factory, logger, leases)...), nil
}

func tick(ctx context.Context, db *gorm.DB, leases domain.LeaseRepository, factory *rpcclient.Factory, owner string, cfg *values.RuntimeConfig, profile *commonv1.HimalayaProfilePolicy) error {
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: values.StrategyKey(profile.UserId, profile.FundId),
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
	book, err := himalayav1.NewMarketClient(market).GetBook(ctx, &himalayav1.BookRequest{Symbol: profile.Symbol})
	if err != nil {
		return err
	}
	portfolio, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServicePortfolio})
	if err != nil {
		return err
	}
	snapshot, err := himalayav1.NewPortfolioClient(portfolio).GetSnapshot(ctx, values.Scope(profile))
	if err != nil {
		return err
	}
	targets, err := BuildTargets(cfg.Himalaya, profile, book, snapshot)
	if err != nil {
		return err
	}
	instance, err := repository.NativeExecutionInstance(ctx, db, values.ScopeKey(profile.UserId, profile.FundId))
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
	_, err = himalayav1.NewExecutionClient(execution).Reconcile(ctx, &himalayav1.QuotePlan{
		Scope:     values.Scope(profile),
		Book:      book,
		Strategy:  values.Authority(token),
		Targets:   targets,
		RequestId: rand.Text(),
	})
	return err
}
