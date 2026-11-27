//go:build integration

package venue

import (
	"context"
	"strconv"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type touchRoomClient struct {
	*directQuoteClient
	ackOnly bool
}

func (c *touchRoomClient) CancelQuote(ctx context.Context, request gateway.CancelRequest) error {
	if c.ackOnly {
		c.cancellations.Add(1)
		return nil // The acknowledgement does not make the order disappear.
	}
	return c.directQuoteClient.CancelQuote(ctx, request)
}

func TestFullSafeBridgeObservesRoomBeforeReplacingBothBestPrices(t *testing.T) {
	for _, mode := range []string{"visible", "ack_only", "expired_source"} {
		t.Run(mode, func(t *testing.T) {
			ackOnly := mode == "ack_only"
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			setup := newCurrentStateTest(ctx, t)
			runtime, profile := setup.runtime, setup.profile
			runtime.cfg.Template = domain.StrategyTemplateMarketMaker
			runtime.store.matching = true
			profile.StrategyInstanceId = setup.instance.Id
			profile.MaximumOrderNotional = "100"
			cycle, wanted := fullSafeBridgeCycle()
			cycle.profile, cycle.lease = profile, setup.lease
			for index, order := range cycle.snapshot.Orders {
				order.OrderId = strconv.Itoa(index + 1)
				order.ClientId = native.InstanceClientPrefix(profile) + order.OrderId
			}
			setup.client.snapshot.Orders = cycle.snapshot.Orders
			client := &touchRoomClient{
				directQuoteClient: &directQuoteClient{currentStateClient: setup.client},
				ackOnly:           ackOnly,
			}
			runtime.client.Store(&gatewaySession{Client: client})
			book, _ := retentionAdmissionPlan(setup)
			book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
			book.Bids = []*common.VenueLevel{{Price: "99.9"}, {Price: "98"}}
			book.Asks = []*common.VenueLevel{{Price: "100.1"}, {Price: "102"}}
			cycle.authority = LiveAuthority{
				InstanceID: setup.instance.Id,
				Revision:   setup.instance.Revision,
				Book:       book,
			}
			cycle.result = &pb.QuoteResult{}
			forbidden := denyMakerOrderStorage(t, setup.db)
			var err error
			cycle.snapshot, err = runtime.refresh(ctx, profile, setup.lease, book)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.makeQuoteRoom(ctx, cycle, wanted); err != nil {
				t.Fatal(err)
			}
			wantDepth := 20
			if ackOnly {
				wantDepth = 21
			}
			depth := cycle.visibleQuoteDepth()
			// Pending acknowledgements are excluded from retention depth, but
			// still consume the original complete snapshot's capacity and risk.
			if depth[domain.SideBuy] != 20 || depth[domain.SideSell] != 20 || client.cancellations.Load() != 2 {
				t.Fatalf("room failed to preserve 20 safe own levels: depth=%v cancels=%d", depth, client.cancellations.Load())
			}
			active := make(map[string]int)
			for _, order := range cycle.snapshot.Orders {
				if native.Active(order.Status) {
					active[order.Side]++
				}
			}
			if active[string(domain.SideBuy)] != wantDepth || active[string(domain.SideSell)] != wantDepth {
				t.Fatalf("room invented freed capacity: %v, want %d", active, wantDepth)
			}
			if mode == "expired_source" {
				book.ValidUntilMs = time.Now().Add(-time.Millisecond).UnixMilli()
			}
			if err := runtime.fillQuoteDepth(ctx, cycle); err != nil {
				t.Fatal(err)
			}
			wantPlaced := uint32(2)
			if mode != "visible" {
				wantPlaced = 0
			}
			if cycle.result.Placed != wantPlaced || client.submissions.Load() != uint64(wantPlaced) || forbidden.Load() != 0 {
				t.Fatalf("touch replacement placed=%d SDK=%d forbidden storage=%d, want %d", cycle.result.Placed, client.submissions.Load(), forbidden.Load(), wantPlaced)
			}
		})
	}
}
