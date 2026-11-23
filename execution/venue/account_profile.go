package venue

import (
	"context"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func (r *Runtime) accountProfile(ctx context.Context, startup *common.VenueProfilePolicy) (*common.VenueProfilePolicy, error) {
	if r.cfg.InstanceID == 0 {
		return startup, nil
	}
	return venueconfig.CurrentProfile(ctx, r.db, startup)
}

func (r *Runtime) quoteAccount(ctx context.Context, startup *common.VenueProfilePolicy) (*common.VenueProfilePolicy, domain.LeaseToken, error) {
	profile, err := r.accountProfile(ctx, startup)
	if err != nil {
		return nil, domain.LeaseToken{}, err
	}
	lease, err := r.acquire(ctx, profile)
	return profile, lease, err
}
