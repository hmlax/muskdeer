package native

import (
	"context"
	"maps"
	"slices"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/logging"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func sortedDependencyKeys[V any](values map[string]V) []string {
	return slices.Sorted(maps.Keys(values))
}

// NewInstances describes only the account observations needed by algorithms.
func NewInstances(ctx context.Context, db *gorm.DB, owner string, instances []*instanceconfig.Instance, factory *rpcclient.Factory, logger *logging.Logger) (*Runtime, error) {
	if ctx == nil {
		return nil, instanceconfig.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent := &Runtime{children: make(map[string]*Runtime)}
	for _, instance := range instances {
		if instance == nil || instance.ID <= 0 {
			return nil, instanceconfig.ErrInvalid
		}
		parent.inventory = append(parent.inventory, &commonv1.VenueInstanceRevision{
			Id:       instance.ID,
			Revision: instance.Revision,
		})
		if !instance.Requirements().Account {
			continue
		}
		settings, err := instance.Settings()
		if err != nil {
			return nil, err
		}
		key := native.ScopeKey(instance.Profile.UserId, instance.Profile.FundId)
		if child := parent.children[key]; child != nil {
			if child.cfg.Venue.Profiles[0].Symbol != instance.Profile.Symbol {
				return nil, instanceconfig.ErrInvalid
			}
			child.cfg.OperationTimeout = max(child.cfg.OperationTimeout, settings.OperationTimeout)
			if settings.Venue.LeaseTtl.AsDuration() > child.cfg.Venue.LeaseTtl.AsDuration() {
				child.cfg.Venue.LeaseTtl = settings.Venue.LeaseTtl
			}
			continue
		}
		parent.children[key] = &Runtime{
			db:      db,
			owner:   owner,
			cfg:     settings,
			factory: factory,
			logger:  logger,
		}
	}
	return parent, nil
}
