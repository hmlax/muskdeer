package venue

import (
	"context"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func (r *Runtime) hasFreshObservation() bool {
	seen, now := r.lastObservation.Load(), time.Now().UnixMilli()
	return seen > 0 && seen <= now && now-seen < native.SnapshotAge.Milliseconds()
}

func (r *Runtime) validateMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan, book *common.VenueBook) error {
	if book.GetValidUntilMs() <= time.Now().UnixMilli() {
		return native.ErrInvalid
	}
	if err := r.validatePlanAuthority(ctx, profile, plan, book); err != nil {
		return err
	}
	if _, err := r.targets(profile, plan); err != nil {
		return err
	}
	if err := r.checkDirectQuoteConfig(ctx, profile); err != nil {
		return err
	}
	budget, err := r.store.Budget(ctx, AccountID(profile.UserId, profile.FundId))
	if err != nil {
		return err
	}
	if !budget.Enabled || budget.UserId != profile.UserId || budget.FundId != profile.FundId || budget.Symbol != profile.Symbol {
		return ErrRiskLimit
	}
	return nil
}
