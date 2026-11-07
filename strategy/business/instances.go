package business

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	nativestrategy "github.com/hmlax/muskdeer/strategy/native"
)

// NewInstances dispatches the selected implementation for every configured row.
func NewInstances(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Strategy, error) {
	if cfg.GetName() != "strategy" {
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
	workers, err := nativestrategy.InstanceWorkers(db, incarnation, instances, factory, owner.Logger())
	if err != nil {
		return nil, err
	}
	return &Strategy{workers: workers}, nil
}
