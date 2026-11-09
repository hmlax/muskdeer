package native

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strconv"
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
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
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
	return configuredWorkers(db, owner, cfg, factory, logger)
}

// InstanceWorkers dispatches independent immutable instance parameters.
func InstanceWorkers(db *gorm.DB, owner string, instances []*instanceconfig.Instance, factory *rpcclient.Factory, logger *logging.Logger) ([]deer.Worker, error) {
	var result []deer.Worker
	for _, instance := range instances {
		settings, err := instance.Settings()
		if err != nil {
			return nil, err
		}
		workers, err := configuredWorkers(db, owner, settings, factory, logger)
		if err != nil {
			return nil, err
		}
		for _, worker := range workers {
			worker.Name += "." + strconv.FormatInt(instance.ID, 10)
			result = append(result, worker)
		}
	}
	return result, nil
}

func configuredWorkers(db *gorm.DB, owner string, cfg *venueconfig.RuntimeConfig, factory *rpcclient.Factory, logger *logging.Logger) ([]deer.Worker, error) {
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	var workers []deer.Worker
	for _, profile := range cfg.Venue.Profiles {
		if cfg.Template == domain.StrategyTemplateWashTrade {
			continue
		}
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
	if active, err := instanceconfig.Active(ctx, db, cfg.InstanceID, cfg.Revision); err != nil || !active {
		return err
	}
	token, err := values.KeepLease(ctx, leases, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: values.StrategyKey(profile.UserId, profile.FundId),
	}, owner, cfg.Venue.LeaseTtl.AsDuration())
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
	targets, err := instanceTargets(cfg, profile, book, snapshot)
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
		Scope:              values.Scope(profile),
		Book:               book,
		Strategy:           values.Authority(token),
		Targets:            targets,
		RequestId:          rand.Text(),
		StrategyInstanceId: cfg.InstanceID,
		StrategyRevision:   cfg.Revision,
	})
	return err
}
