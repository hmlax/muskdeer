package business

import (
	"context"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/execution/adapters/himalaya"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

// NewInstances shares one SDK writer session and one durable budget per fund.
func NewInstances(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Execution, error) {
	if cfg.GetName() != "execution" {
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
	runtime, err := venueexecution.NewInstances(ctx, instances, venueexecution.Dependencies{
		DB:         db,
		OwnerID:    incarnation,
		RPC:        factory,
		Logger:     owner.Logger(),
		Gateway:    himalaya.Factory{},
		Connection: cfg.VenueConnection,
	})
	if err != nil {
		return nil, err
	}
	return &Execution{
		instances: runtime,
		workers:   runtime.Workers(),
	}, nil
}
