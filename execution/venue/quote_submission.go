package venue

import (
	"context"
	"crypto/rand"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// quoteCycle is owned by one admitted reconcile call; all request pointers are
// borrowed and immutable, while retention and result belong only to that call.
type quoteCycle struct {
	profile   *commonv1.HimalayaProfilePolicy
	lease     domain.LeaseToken
	authority LiveAuthority
	targets   []*commonv1.HimalayaTarget
	snapshot  *commonv1.HimalayaSnapshot
	retained  *quoteRetention
	result    *executionvenuev1.QuoteResult
	admitted  []*commonv1.HimalayaTarget
}

func (r *Runtime) submitQuote(ctx context.Context, cycle *quoteCycle, target *commonv1.HimalayaTarget) error {
	price, e1 := native.Value(target.Price, false, false)
	quantity, e2 := native.Value(target.Quantity, false, false)
	if e1 != nil || e2 != nil {
		return ErrInvalidSubmit
	}
	prepared, err := r.store.PrepareLive(ctx, cycle.lease, SubmitRequest{
		UserID:   cycle.profile.UserId,
		FundID:   cycle.profile.FundId,
		Symbol:   cycle.profile.Symbol,
		ClientID: native.ClientPrefix + cycle.profile.Symbol + "_" + rand.Text(),
		Side:     domain.Side(target.Side),
		Price:    price,
		Quantity: quantity,
		Leverage: cycle.profile.Leverage,
	}, cycle.authority)
	if err != nil {
		return err
	}
	response, err := r.client.Load().Submit(ctx, prepared.Request)
	if err != nil {
		return err
	}
	if err := r.store.Acknowledge(ctx, prepared.Admission, response); err != nil {
		return err
	}
	cycle.result.Placed++
	cycle.admitted = append(cycle.admitted, target)
	return nil
}

func (r *Runtime) seedQuoteSides(ctx context.Context, cycle *quoteCycle) error {
	pending, err := r.unresolvedCancels(ctx, cycle.lease.Key.AccountID)
	if err != nil || pending {
		return err
	}
	for _, target := range quoteSeedTargets(cycle.snapshot, cycle.targets, cycle.retained) {
		if !cycle.canPlace(target) {
			continue
		}
		if err := r.submitQuote(ctx, cycle, target); err != nil {
			return err
		}
		price, _ := native.Value(target.Price, false, false)
		cycle.retained.targets[target.Side+"/"+price.String()] = true
	}
	return nil
}

func quoteSeedTargets(snapshot *commonv1.HimalayaSnapshot, targets []*commonv1.HimalayaTarget, retained *quoteRetention) []*commonv1.HimalayaTarget {
	present := make(map[domain.Side]bool)
	bestBid, bestAsk := decimal.Zero, decimal.Zero
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		side := domain.Side(order.Side)
		if retained.orders[order.OrderId] {
			present[side] = true
		}
		price, _ := native.Value(order.Price, false, false)
		if side == domain.SideBuy {
			bestBid = decimal.Max(bestBid, price)
		} else if bestAsk.IsZero() || price.LessThan(bestAsk) {
			bestAsk = price
		}
	}
	seeds := make([]*commonv1.HimalayaTarget, 0, 2)
	for _, target := range quoteSubmissionOrder(targets) {
		side := domain.Side(target.Side)
		if present[side] {
			continue
		}
		price, _ := native.Value(target.Price, false, false)
		if side == domain.SideBuy && !bestAsk.IsZero() && !price.LessThan(bestAsk) ||
			side == domain.SideSell && !bestBid.IsZero() && !price.GreaterThan(bestBid) {
			continue
		}
		seeds = append(seeds, target)
		present[side] = true
	}
	return seeds
}
