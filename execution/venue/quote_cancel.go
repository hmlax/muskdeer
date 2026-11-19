package venue

import (
	"context"
	"errors"
	"sync"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

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
	failures := make([]error, len(orders))
	var group sync.WaitGroup
	for index, order := range orders {
		group.Go(func() { failures[index] = r.sendQuoteCancellation(ctx, lease, profile, order) })
	}
	group.Wait()
	for _, err := range failures {
		if err == nil {
			result.Cancelled++
		}
	}
	return errors.Join(failures...)
}

func (r *Runtime) sendQuoteCancellation(ctx context.Context, lease domain.LeaseToken, profile *common.VenueProfilePolicy, order *common.VenueOrder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.quoteSlots != nil {
		select {
		case r.quoteSlots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-r.quoteSlots }()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.cancelOne(ctx, lease, profile, order)
}
