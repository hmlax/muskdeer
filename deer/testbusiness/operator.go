package testbusiness

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func newOperator(ctx context.Context, db *gorm.DB, owner *deer.Deer, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	runtime := &Runtime{}
	add := func(module *Runtime) {
		runtime.modules = append(runtime.modules, module)
		runtime.workers = append(runtime.workers, module.Workers()...)
	}
	if cfg.Market != nil {
		module, err := newMarket(ctx, db, incarnation, cfg)
		if err != nil {
			return nil, err
		}
		add(module)
	}
	if ownerConfigNeedsEvents(cfg) {
		events, err := owner.JetStream()
		if err != nil {
			return nil, err
		}
		module, err := newExecution(ctx, db, events, incarnation, cfg)
		if err != nil {
			return nil, err
		}
		add(module)
		if cfg.PortfolioConsumer != "" {
			module, err = newPortfolio(ctx, db, owner, events, cfg)
			if err != nil {
				return nil, err
			}
			add(module)
		}
	}
	module, err := newStrategy(ctx, db, owner, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	module.trustedAdmin = true
	add(module)
	return runtime, nil
}

func ownerConfigNeedsEvents(cfg *configv1.BusinessRuntime) bool {
	return len(cfg.Strategy.AccountIds) != 0 || cfg.Risk != nil || cfg.Simulator != nil || cfg.PortfolioConsumer != ""
}
