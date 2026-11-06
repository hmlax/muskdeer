package native

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	portfoliovenuev1 "github.com/hmlax/muskdeer/deer/pb/portfolio/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// Workers describes one fenced, bounded maker actor per configured fund.
func Workers(ctx context.Context, db *gorm.DB, owner string, input *configv1.BusinessRuntime, factory *rpcclient.Factory, logger *logging.Logger) ([]deer.Worker, error) {
	cfg, err := venueconfig.LoadRuntime(ctx, db, input, nil)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	var workers []deer.Worker
	for _, profile := range cfg.Venue.Profiles {
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

func tick(ctx context.Context, db *gorm.DB, leases domain.LeaseRepository, factory *rpcclient.Factory, owner string, cfg *venueconfig.RuntimeConfig, profile *commonv1.VenueProfilePolicy) error {
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: values.StrategyKey(profile.UserId, profile.FundId),
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
	book, err := marketvenuev1.NewMarketClient(market).GetBook(ctx, &marketvenuev1.BookRequest{Symbol: profile.Symbol})
	if err != nil {
		return err
	}
	portfolio, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServicePortfolio})
	if err != nil {
		return err
	}
	snapshot, err := portfoliovenuev1.NewPortfolioClient(portfolio).GetSnapshot(ctx, values.Scope(profile))
	if err != nil {
		return err
	}
	targets, err := BuildTargets(cfg.Venue, profile, book, snapshot)
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
	_, err = executionvenuev1.NewExecutionClient(execution).Reconcile(ctx, &executionvenuev1.QuotePlan{
		Scope:     values.Scope(profile),
		Book:      book,
		Strategy:  values.Authority(token),
		Targets:   targets,
		RequestId: rand.Text(),
	})
	return err
}
