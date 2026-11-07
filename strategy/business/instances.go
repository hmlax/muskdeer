package business

import (
	"context"
	"slices"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/strategy/console"
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
	result := &Strategy{workers: workers}
	// The server's CA-verified peer allowlist is the explicit console opt-in.
	if slices.Contains(cfg.GetGrpcSecurity().GetAllowedPeerServices(), rpcsecurity.ServiceOperator) {
		revisions := make(map[int64]int64, len(instances))
		for _, instance := range instances {
			revisions[instance.ID] = instance.Revision
		}
		result.console, err = console.New(console.Dependencies{
			DB:         db,
			Factory:    factory,
			Logger:     owner.Logger(),
			Controls:   true,
			Connection: cfg.VenueConnection,
			Revisions:  revisions,
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
