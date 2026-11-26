package venue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// These bounds limit concurrent transport operations, never configured instances.
const (
	maximumQuoteWave            = 4
	maximumConcurrentQuoteSends = 16
	maximumOptionalQuoteSends   = maximumQuoteWave
)

var errQuoteRefreshBusy = errors.New("quote refresh transport busy")

// Optional refreshes must not queue ahead of a fund that is losing safe depth.
// Count only venue-visible, unique, own, non-crossing levels; acknowledgements
// and other instances' orders cannot turn a shallow side into a full side.
func (cycle *quoteCycle) needsQuoteDepth() bool {
	desired := make(map[domain.Side]int)
	for _, target := range cycle.targets {
		desired[domain.Side(target.Side)]++
	}
	visible := cycle.visibleQuoteDepth()
	return visible[domain.SideBuy] < desired[domain.SideBuy] || visible[domain.SideSell] < desired[domain.SideSell]
}

// A full side can still lack its current best target. Only that missing touch
// bypasses optional refresh admission; an existing touch with a size change
// remains optional. Another actor's order cannot stand in for our replacement.
func (cycle *quoteCycle) needsQuoteTouch(request gateway.QuoteRequest) bool {
	best := quoteBestTargets(cycle.targets)[request.Side]
	price, err := native.Value(best.GetPrice(), false, false)
	if err != nil || !price.Equal(request.Price) {
		return false
	}
	for _, order := range cycle.snapshot.Orders {
		if cycle.cancelling[order.OrderId] || !native.Active(order.Status) || order.Side != string(request.Side) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
			continue
		}
		_, quantity, e1 := orderKey(order)
		visible, e2 := native.Value(order.Price, false, false)
		if e1 == nil && e2 == nil && quantity.IsPositive() && visible.Equal(price) {
			return false
		}
	}
	return true
}

func (cycle *quoteCycle) visibleQuoteDepth() map[domain.Side]int {
	best := make(map[domain.Side]decimal.Decimal)
	for _, target := range cycle.targets {
		price, _ := native.Value(target.Price, false, false)
		side := domain.Side(target.Side)
		if previous := best[side]; previous.IsZero() || side == domain.SideBuy && price.GreaterThan(previous) || side == domain.SideSell && price.LessThan(previous) {
			best[side] = price
		}
	}
	visible := make(map[domain.Side]int)
	seen := make(map[string]bool)
	limit, err := native.Value(cycle.profile.MaximumOrderNotional, false, false)
	if err != nil {
		return nil
	}
	for _, order := range cycle.snapshot.Orders {
		if cycle.cancelling[order.OrderId] || !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.InstanceClientPrefix(cycle.profile)) {
			continue
		}
		if _, safe := bridgePrice(order, best, limit); !safe {
			continue
		}
		key, _, err := orderKey(order)
		if err == nil && !seen[key] {
			visible[domain.Side(order.Side)]++
			seen[key] = true
		}
	}
	return visible
}

func (r *Runtime) fillDirectQuoteDepth(ctx context.Context, cycle *quoteCycle) error {
	cycle.targets = cycle.quoteMakerMaintenanceOrder()
	for cursor := 0; cursor < len(cycle.targets); {
		reserved := len(cycle.admitted)
		requests := make([]gateway.QuoteRequest, 0, maximumQuoteWave)
		for cursor < len(cycle.targets) && len(requests) < maximumQuoteWave {
			target := cycle.targets[cursor]
			cursor++
			price, _ := native.Value(target.Price, false, false)
			if cycle.retained.targets[target.Side+"/"+price.String()] {
				continue
			}
			if !cycle.canPlace(target) {
				cycle.retained.pending = true
				continue
			}
			deadline, bounded := ctx.Deadline()
			remaining := time.Until(time.UnixMilli(min(cycle.authority.Book.ValidUntilMs, cycle.snapshot.ValidUntilMs)))
			if !bounded || min(remaining, time.Until(deadline)) <= minimumLiveSDKWindow+50*time.Millisecond {
				cycle.retained.pending = true
				cursor = len(cycle.targets)
				break
			}
			request, err := r.prepareDirectQuote(ctx, cycle, target)
			if err != nil {
				cycle.admitted = cycle.admitted[:reserved]
				return err
			}
			// Reserve each candidate before selecting the next: capacity, crossing
			// checks and the full aggregate budget include all in-flight siblings.
			cycle.admitted = append(cycle.admitted, target)
			requests = append(requests, request)
		}
		if err := r.sendQuoteWave(ctx, cycle, requests); err != nil {
			return err
		}
		if r.quotePriceMoved(cycle) {
			// Finish observation/retirement for this joined wave, then let the
			// same writer take the newest target instead of sending old depth.
			cycle.retained.pending = true
			break
		}
	}
	return nil
}

func (r *Runtime) sendQuoteWave(ctx context.Context, cycle *quoteCycle, requests []gateway.QuoteRequest) error {
	width, release := quoteWaveSize(ctx, min(maximumQuoteWave, len(requests)))
	defer release()
	for cursor := 0; cursor < len(requests); cursor += width {
		batch := requests[cursor:min(cursor+width, len(requests))]
		results := make([]error, len(batch))
		var group sync.WaitGroup
		for index, request := range batch {
			group.Go(func() { results[index] = r.sendDirectQuote(ctx, cycle, request) })
		}
		// Join every accepted sibling before releasing any capacity or writer.
		// Never replay an ambiguous send or dispatch the remaining batch on failure.
		group.Wait()
		var failures []error
		for _, err := range results {
			if err == nil {
				cycle.result.Placed++
				continue
			}
			if errors.Is(err, errQuoteRefreshBusy) {
				cycle.retained.pending = true
				continue
			}
			failures = append(failures, err)
		}
		if len(failures) > 0 {
			return errors.Join(failures...)
		}
	}
	return nil
}
