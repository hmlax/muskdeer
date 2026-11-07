package business

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/market/usdm"
)

// NewInstances assembles current database instance dependencies without starting tasks.
func NewInstances(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Market, error) {
	if cfg.GetName() != "market" {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	db, incarnation, instances, err := runtimeconfig.InstancesData(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}

	runtime, err := usdm.NewInstances(ctx, db, incarnation, instances, owner.Logger())
	if err != nil {
		return nil, err
	}
	return &Market{
		native:  runtime,
		workers: runtime.Workers(),
	}, nil
}
