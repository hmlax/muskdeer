package business

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	nativeportfolio "github.com/hmlax/muskdeer/portfolio/native"
)

// NewInstances assembles current database instance dependencies without starting tasks.
func NewInstances(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Portfolio, error) {
	if cfg.GetName() != "portfolio" {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	db, incarnation, instances, err := runtimeconfig.InstancesData(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	factory, err := owner.GRPCClients()
	if err != nil {
		return nil, err
	}
	runtime, err := nativeportfolio.NewInstances(ctx, db, incarnation, instances, factory, owner.Logger())
	if err != nil {
		return nil, err
	}
	return &Portfolio{
		native:  runtime,
		workers: runtime.Workers(),
	}, nil
}
