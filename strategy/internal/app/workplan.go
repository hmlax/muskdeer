package app

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/business"
)

// WorkPlan assembles only this service's typed runtime after Deer opens dependencies.
type WorkPlan struct {
	config  *Config
	runtime *business.Strategy
}

// NewWorkPlan describes owned work without opening another pool or starting tasks.
func NewWorkPlan(cfg *Config) *WorkPlan { return &WorkPlan{config: cfg} }

func (p *WorkPlan) configure(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
	runtime, err := business.NewStrategy(ctx, p.config.service, owner)
	if err != nil {
		return nil, err
	}
	p.runtime = runtime
	return runtime.Workers(), nil
}
