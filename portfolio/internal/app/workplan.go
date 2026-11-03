package app

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/portfolio/business"
)

// WorkPlan assembles only this service's typed runtime after Deer opens dependencies.
type WorkPlan struct {
	config  *Config
	runtime *business.Portfolio
}

func (p *WorkPlan) register(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
	if p.runtime == nil {
		return errors.New("portfolio work plan requires initialization")
	}
	return p.runtime.RegisterGRPC(registrar)
}

// NewWorkPlan describes owned work without opening another pool or starting tasks.
func NewWorkPlan(cfg *Config) *WorkPlan { return &WorkPlan{config: cfg} }

func (p *WorkPlan) configure(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
	runtime, err := business.NewPortfolio(ctx, p.config.service, owner)
	if err != nil {
		return nil, err
	}
	p.runtime = runtime
	return runtime.Workers(), nil
}
