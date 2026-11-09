package venue

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

// A source frame expires independently of the strategy owner's permission to
// retain existing quotes. New SDK sends still require an unexpired source frame.
type quotePlanAuthority struct {
	strategy *common.VenueAuthority
	source   *common.VenueAuthority
}

type quoteObservation struct {
	lease    domain.LeaseToken
	book     *common.VenueBook
	snapshot *common.VenueSnapshot
}

func (r *Runtime) rememberQuotePlan(profile *common.VenueProfilePolicy, plan *execution.QuotePlan, book *common.VenueBook) {
	until := book.ValidUntilMs
	if r.cfg.InstanceID > 0 {
		r.quotePlan.Store(&quotePlanAuthority{
			strategy: proto.CloneOf(plan.Strategy),
			source:   proto.CloneOf(book.Publisher),
		})
		if r.store.matching {
			until = time.Now().Add(maximumOperationTimeout).UnixMilli()
		} else {
			until = min(plan.Strategy.ExpiresAtMs, time.Now().Add(r.cfg.Venue.LeaseTtl.AsDuration()).UnixMilli())
		}
	}
	r.plans[AccountID(profile.UserId, profile.FundId)].Store(until)
}

func (r *Runtime) currentQuotePlan(ctx context.Context, book *common.VenueBook, account string) bool {
	if r.plans[account].Load() <= time.Now().UnixMilli() {
		return false
	}
	if r.cfg.InstanceID == 0 {
		return true
	}
	plan := r.quotePlan.Load()
	if r.store.matching {
		return plan != nil && book.GetValidUntilMs() > time.Now().UnixMilli() && len(book.Bids) > 0 && len(book.Asks) > 0 &&
			plan.source.GetAccountId() == book.GetPublisher().GetAccountId()
	}
	return plan != nil && book.GetValidUntilMs() > time.Now().UnixMilli() && len(book.Bids) > 0 && len(book.Asks) > 0 &&
		plan.source.GetAccountId() == book.Publisher.GetAccountId() &&
		plan.source.GetOwnerId() == book.Publisher.GetOwnerId() && plan.source.GetEpoch() == book.Publisher.GetEpoch() &&
		repository.CheckNativeAuthority(ctx, r.db, plan.strategy) == nil && repository.CheckNativeAuthority(ctx, r.db, book.Publisher) == nil
}
