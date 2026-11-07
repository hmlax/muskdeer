package usdm

import (
	"context"
	"maps"
	"slices"

	"github.com/hmlax/muskdeer/deer/logging"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"gorm.io/gorm"
)

func sortedDependencyKeys[V any](values map[string]V) []string {
	return slices.Sorted(maps.Keys(values))
}

// NewInstances deduplicates actual source dependencies; strategy knobs are not merged.
func NewInstances(ctx context.Context, db *gorm.DB, owner string, instances []*instanceconfig.Instance, logger *logging.Logger) (*Runtime, error) {
	if ctx == nil {
		return nil, instanceconfig.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	parent := &Runtime{children: make(map[string]*Runtime)}
	for _, instance := range instances {
		if instance.Profile.MarketDataSource != native.Source {
			return nil, instanceconfig.ErrInvalid
		}
		settings, err := instance.Settings()
		if err != nil {
			return nil, err
		}
		key := instance.Profile.Symbol
		child := parent.children[key]
		if child == nil {
			child = &Runtime{
				db:     db,
				owner:  owner,
				cfg:    settings,
				leases: leases,
				books:  make(map[string]*commonv1.VenueBook),
				trades: make(map[string]*commonv1.VenueTrade),
				logger: logger,
			}
			parent.children[key] = child
		} else {
			child.cfg.OperationTimeout = max(child.cfg.OperationTimeout, settings.OperationTimeout)
			if settings.Venue.LeaseTtl.AsDuration() > child.cfg.Venue.LeaseTtl.AsDuration() {
				child.cfg.Venue.LeaseTtl = settings.Venue.LeaseTtl
			}
		}
		if instance.Requirements().Trades {
			child.cfg.Venue.SyntheticTape = settings.Venue.SyntheticTape
		}
	}
	return parent, nil
}
