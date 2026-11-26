package venue

import (
	"context"
	"errors"
	"sync"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func (r *Runtime) cancelQuoteRetirements(ctx context.Context, cycle *quoteCycle, orders []*common.VenueOrder) error {
	if !r.directMaker() {
		return r.cancelOrders(ctx, cycle.lease, cycle.profile, orders, cycle.result)
	}
	for cursor := 0; cursor < len(orders); cursor += maximumQuoteWave {
		if err := ctx.Err(); err != nil {
			return err
		}
		deferRetirement, err := r.deferQuoteRetirement(cycle)
		if err != nil {
			return err
		}
		if deferRetirement {
			cycle.retained.pending = true
			return nil
		}
		if err := r.cancelQuoteWave(ctx, cycle.lease, cycle.profile, orders[cursor:min(cursor+maximumQuoteWave, len(orders))], cycle.result); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) cancelOrders(ctx context.Context, lease domain.LeaseToken, profile *common.VenueProfilePolicy, orders []*common.VenueOrder, result *pb.QuoteResult) error {
	if r.directMaker() {
		for cursor := 0; cursor < len(orders); cursor += maximumQuoteWave {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.cancelQuoteWave(ctx, lease, profile, orders[cursor:min(cursor+maximumQuoteWave, len(orders))], result); err != nil {
				return err
			}
		}
		return nil
	}
	for _, order := range orders {
		if err := r.cancelOne(ctx, lease, profile, order); err != nil {
			return err
		}
		result.Cancelled++
	}
	return nil
}

// Selection already assumes every cancellation succeeds. Concurrency changes
// neither that worst-case floor nor the requirement for visible replacements.
func (r *Runtime) cancelQuoteWave(ctx context.Context, lease domain.LeaseToken, profile *common.VenueProfilePolicy, orders []*common.VenueOrder, result *pb.QuoteResult) error {
	work, width, release, err := r.reserveQuoteWave(ctx, min(maximumQuoteWave, len(orders)), quoteOperationCancel, time.Time{})
	if err != nil {
		return err
	}
	ctx = work
	defer release()
	for cursor := 0; cursor < len(orders); cursor += width {
		batch := orders[cursor:min(cursor+width, len(orders))]
		failures := make([]error, len(batch))
		var group sync.WaitGroup
		for index, order := range batch {
			group.Go(func() { failures[index] = r.sendQuoteCancellation(ctx, lease, profile, order) })
		}
		group.Wait()
		for _, err := range failures {
			if err == nil {
				result.Cancelled++
			}
		}
		if err := errors.Join(failures...); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) sendQuoteCancellation(ctx context.Context, lease domain.LeaseToken, profile *common.VenueProfilePolicy, order *common.VenueOrder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	finishAdmission := r.traceQuoteAdmission(ctx, quoteOperationCancel)
	if r.quoteBudget != nil {
		work, release, err := r.reserveQuoteSend(ctx)
		if err != nil {
			finishAdmission(err)
			return err
		}
		defer release()
		ctx = work
	} else if r.quoteSlots != nil {
		select {
		case r.quoteSlots <- struct{}{}:
		case <-ctx.Done():
			finishAdmission(ctx.Err())
			return ctx.Err()
		}
		defer func() { <-r.quoteSlots }()
	}
	finishAdmission(nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.cancelOne(ctx, lease, profile, order)
}
