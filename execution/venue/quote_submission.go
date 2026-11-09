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
	profile     *commonv1.VenueProfilePolicy
	bridgeSlots uint32
	lease       domain.LeaseToken
	authority   LiveAuthority
	targets     []*commonv1.VenueTarget
	snapshot    *commonv1.VenueSnapshot
	retained    *quoteRetention
	result      *executionvenuev1.QuoteResult
	admitted    []*commonv1.VenueTarget
}

func (r *Runtime) submitQuote(ctx context.Context, cycle *quoteCycle, target *commonv1.VenueTarget) error {
	if r.directMaker() {
		return r.submitDirectQuote(ctx, cycle, target)
	}
	price, e1 := native.Value(target.Price, false, false)
	quantity, e2 := native.Value(target.Quantity, false, false)
	if e1 != nil || e2 != nil {
		return ErrInvalidSubmit
	}
	if err := r.gridSubmissionRisk(cycle, target); err != nil {
		return err
	}
	prepared, err := r.store.PrepareLive(ctx, cycle.lease, SubmitRequest{
		UserID:   cycle.profile.UserId,
		FundID:   cycle.profile.FundId,
		Symbol:   cycle.profile.Symbol,
		ClientID: native.InstanceClientPrefix(cycle.profile) + rand.Text(),
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

func quoteSeedTargets(snapshot *commonv1.VenueSnapshot, targets []*commonv1.VenueTarget, retained *quoteRetention) []*commonv1.VenueTarget {
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
	seeds := make([]*commonv1.VenueTarget, 0, 2)
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

// Include every active order plus earlier submissions before adding grid risk.
func (r *Runtime) gridSubmissionRisk(cycle *quoteCycle, target *commonv1.VenueTarget) error {
	if r.cfg.Grid == nil {
		return nil
	}
	limit, err := native.Value(r.cfg.Grid.InventoryLimit, false, false)
	if err != nil {
		return err
	}
	net, total := decimal.Zero, decimal.Zero
	for _, position := range cycle.snapshot.Positions {
		quantity, err := native.Value(position.SignedQuantity, true, true)
		if err != nil {
			return err
		}
		net = net.Add(quantity)
	}
	buys, sells := decimal.Zero, decimal.Zero
	for _, order := range cycle.snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		quantity, e1 := native.Value(order.Quantity, false, false)
		executed, e2 := native.Value(order.ExecutedQuantity, false, true)
		price, e3 := native.Value(order.Price, false, false)
		if e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) {
			return native.ErrInvalid
		}
		quantity = quantity.Sub(executed)
		total = total.Add(price.Mul(quantity))
		if order.Side == string(domain.SideBuy) {
			buys = buys.Add(quantity)
		} else {
			sells = sells.Add(quantity)
		}
	}
	for _, admitted := range append(append([]*commonv1.VenueTarget{}, cycle.admitted...), target) {
		q, _ := native.Value(admitted.Quantity, false, false)
		p, _ := native.Value(admitted.Price, false, false)
		total = total.Add(p.Mul(q))
		if admitted.Side == string(domain.SideBuy) {
			buys = buys.Add(q)
		} else {
			sells = sells.Add(q)
		}
	}
	budget, err := native.Value(r.cfg.Grid.TotalBudget, false, false)
	if err != nil || total.GreaterThan(budget) || net.Add(buys).GreaterThan(limit) || net.Sub(sells).LessThan(limit.Neg()) {
		return ErrRiskLimit
	}
	return nil
}
