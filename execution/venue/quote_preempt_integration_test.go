//go:build integration

package venue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestMakerNewBestPricePreemptsObsoleteDeepWave(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	runtime, cycle, client, setup := batchQuoteSetup(ctx, t, 8)
	runtime.mailbox = newQuoteMailbox()
	runtime.closing = &atomic.Bool{}
	cycle.plan = &pb.QuotePlan{
		Scope:              native.Scope(setup.profile),
		StrategyInstanceId: setup.instance.Id,
		StrategyRevision:   setup.instance.Revision,
		Strategy:           &common.VenueAuthority{AccountId: native.StrategyKey(setup.profile.UserId, setup.profile.FundId)},
		Book:               proto.CloneOf(cycle.authority.Book),
		Targets:            cycle.targets,
	}
	cycle.plan.Book.Sequence = 10
	cycle.plan.Book.Symbol, cycle.plan.Book.Source, cycle.plan.Book.Segment = setup.profile.Symbol, native.Source, native.Segment
	cycle.plan.Book.Publisher = &common.VenueAuthority{AccountId: native.MarketKey(setup.profile.Symbol)}
	cycle.plan.Book.Bids = []*common.VenueLevel{{Price: "99"}, {Price: "98.4"}}
	cycle.plan.Book.Asks = []*common.VenueLevel{{Price: "101.1"}, {Price: "101.7"}}
	done := make(chan error, 1)
	go func() { done <- runtime.fillQuoteDepth(ctx, cycle) }()
	for range 4 {
		select {
		case <-client.started:
		case <-ctx.Done():
			err := <-done
			t.Fatalf("first quote wave failed to start: %v", err)
		}
	}
	next := proto.CloneOf(cycle.plan)
	next.Book.Sequence++
	next.Targets[0].Price = "99.1"
	next.Book.Bids[0].Price = "99.1"
	if err := runtime.admitMakerIntent(ctx, setup.profile, next, next.Book); err != nil {
		cancel()
		joined := <-done
		t.Fatalf("new target was not admitted: %v (joined: %v)", err, joined)
	}
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if client.calls.Load() != 4 || cycle.result.Placed != 4 || !cycle.retained.pending || client.active.Load() != 0 {
		t.Fatalf("obsolete wave continued: calls=%d placed=%d pending=%t active=%d", client.calls.Load(), cycle.result.Placed, cycle.retained.pending, client.active.Load())
	}
	if runtime.mailbox.pending.Load().Targets[0].Price != "99.1" || len(runtime.mailbox.wake) != 1 {
		t.Fatal("new price was lost instead of remaining pending for the same writer")
	}
}
