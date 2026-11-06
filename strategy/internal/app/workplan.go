package app

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/strategy/business"
)

// WorkPlan assembles only this service's typed runtime after Deer opens dependencies.
type WorkPlan struct {
	config     *Config
	runtime    *business.Strategy
	configured bool
}

// NewWorkPlan describes owned work without opening another pool or starting tasks.
func NewWorkPlan(cfg *Config) *WorkPlan { return &WorkPlan{config: cfg} }

func (p *WorkPlan) configure(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
	cfg, err := runtimeconfig.ResolveDatabasePolicy(ctx, p.config.service, owner)
	if err != nil {
		return nil, err
	}
	p.configured = true
	if !cfg.GetBusiness().GetEnabled() {
		return nil, nil
	}
	runtime, err := business.NewStrategy(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	p.runtime = runtime
	return runtime.Workers(), nil
}
