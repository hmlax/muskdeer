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

// A validated intent may keep existing safe quotes while a fund writer is
// occupied. It grants no SDK admission: normal reconciliation still re-reads
// the venue and validates all risk under the exclusive writer gate.
func (r *Runtime) retainPlanWhileBusy(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan, book *common.VenueBook) error {
	if !r.hasFreshObservation() || r.closing.Load() {
		return nil
	}
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
	if r.hasFreshObservation() && !r.closing.Load() && book.ValidUntilMs > time.Now().UnixMilli() && plan.Book.ValidUntilMs > time.Now().UnixMilli() {
		r.rememberQuotePlan(profile, plan, book)
	}
	return nil
}
