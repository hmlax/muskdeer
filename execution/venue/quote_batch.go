package venue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// These bounds limit concurrent transport operations, never configured instances.
const (
	maximumQuoteWave            = 4
	maximumConcurrentQuoteSends = 16
)

func (r *Runtime) fillDirectQuoteDepth(ctx context.Context, cycle *quoteCycle) error {
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
	}
	return nil
}

func (r *Runtime) sendQuoteWave(ctx context.Context, cycle *quoteCycle, requests []gateway.QuoteRequest) error {
	results := make([]error, len(requests))
	var group sync.WaitGroup
	for index, request := range requests {
		group.Go(func() { results[index] = r.sendDirectQuote(ctx, cycle, request) })
	}
	// A failure cannot release the fund writer while siblings still send. Never
	// retry an ambiguous send; the next cycle starts from a new venue observation.
	group.Wait()
	for _, err := range results {
		if err == nil {
			cycle.result.Placed++
		}
	}
	return errors.Join(results...)
}
