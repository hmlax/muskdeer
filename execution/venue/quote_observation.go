package venue

import (
	"context"
	"sync/atomic"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// The single fund writer owns snapshot. Joined SDK siblings only set dirty.
// This attempt-local observation is never persisted or reused by a later turn.
type quoteAttemptObservation struct {
	snapshot *common.VenueSnapshot
	dirty    atomic.Bool
}

type quoteAttemptObservationKey struct{}

func withQuoteAttemptObservation(ctx context.Context) context.Context {
	return context.WithValue(ctx, quoteAttemptObservationKey{}, &quoteAttemptObservation{})
}

func rememberQuoteObservation(ctx context.Context, snapshot *common.VenueSnapshot) {
	if observation, _ := ctx.Value(quoteAttemptObservationKey{}).(*quoteAttemptObservation); observation != nil {
		observation.snapshot = snapshot
		observation.dirty.Store(false)
	}
}

func markQuoteObservationDirty(ctx context.Context) {
	if observation, _ := ctx.Value(quoteAttemptObservationKey{}).(*quoteAttemptObservation); observation != nil {
		observation.dirty.Store(true)
	}
}

func cachedQuoteObservation(ctx context.Context, profile *common.VenueProfilePolicy) *common.VenueSnapshot {
	observation, _ := ctx.Value(quoteAttemptObservationKey{}).(*quoteAttemptObservation)
	if observation == nil || observation.dirty.Load() || profile == nil {
		return nil
	}
	snapshot := observation.snapshot
	scope, market := snapshot.GetScope(), snapshot.GetMarket()
	now := time.Now().UnixMilli()
	if scope.GetUserId() != profile.UserId || scope.GetFundId() != profile.FundId || scope.GetSymbol() != profile.Symbol ||
		market.GetPriceTick() != profile.PriceTick || market.GetQuantityStep() != profile.QuantityStep || profile.Leverage > market.GetMaximumLeverage() ||
		snapshot.GetStartedAtMs() <= 0 || snapshot.GetObservedAtMs() < snapshot.StartedAtMs || snapshot.ObservedAtMs > now ||
		snapshot.GetValidUntilMs() <= now || snapshot.ValidUntilMs > snapshot.StartedAtMs+native.SnapshotAge.Milliseconds() {
		return nil
	}
	return snapshot
}
