//go:build integration

package venue

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/logging"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestFullUnsafeSideMakesObservedRoomBeforeDrainingItsLastOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	runtime, profile := setup.runtime, setup.profile
	runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	runtime.store.matching = true
	profile.StrategyInstanceId = setup.instance.Id
	profile.MaximumOrderNotional = "100"
	logger, err := logging.New(ctx, nil, logging.Identity{Service: "execution", Instance: "room-test"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.logger = logger
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	client := &directQuoteClient{currentStateClient: setup.client, minimumBid: 21, watchBid: true}
	client.snapshot.Orders = nil
	for level := range 21 {
		client.snapshot.Orders = append(client.snapshot.Orders, &common.VenueOrder{
			OrderId:          strconv.Itoa(level + 1),
			ClientId:         native.InstanceClientPrefix(profile) + strconv.Itoa(level+1),
			Side:             string(domain.SideBuy),
			Price:            decimal.NewFromInt(101).Add(decimal.NewFromInt(int64(level)).Shift(-1)).String(),
			Quantity:         "0.1",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
	}
	runtime.client.Store(&gatewaySession{Client: client})
	book, plan := retentionAdmissionPlan(setup)
	book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	book.Bids = []*common.VenueLevel{{Price: "99"}, {Price: "97.1"}}
	book.Asks = []*common.VenueLevel{{Price: "100"}, {Price: "101.9"}}
	plan.Targets = nil
	for level := range 20 {
		for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
			price := decimal.NewFromInt(99).Sub(decimal.NewFromInt(int64(level)).Shift(-1))
			if side == domain.SideSell {
				price = decimal.NewFromInt(100).Add(decimal.NewFromInt(int64(level)).Shift(-1))
			}
			plan.Targets = append(plan.Targets, &common.VenueTarget{Side: string(side), Price: price.String(), Quantity: "0.1"})
		}
	}
	wanted, err := runtime.targets(profile, plan)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.refresh(ctx, profile, setup.lease, book)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := retainQuoteOrders(profile, snapshot, wanted, true, runtime.cfg.ReplacementFraction)
	if err != nil {
		t.Fatal(err)
	}
	cycle := &quoteCycle{
		profile:     profile,
		lease:       setup.lease,
		bridgeSlots: 1,
		snapshot:    snapshot,
		retained:    retained,
		targets:     plan.Targets,
		authority:   LiveAuthority{InstanceID: setup.instance.Id, Revision: setup.instance.Revision, Book: book},
		result:      &pb.QuoteResult{},
	}
	if cycle.canPlace(plan.Targets[0]) {
		t.Fatal("fixture did not fill the bounded side")
	}
	if _, err := runtime.maintainCurrentDepth(ctx, cycle, wanted, true); err != nil {
		t.Fatal(err)
	}
	if client.minimumBid == 0 || client.submissions.Load() == 0 {
		t.Fatalf("side drained before a safe replacement: minimum=%d placements=%d", client.minimumBid, client.submissions.Load())
	}
	if client.cancellations.Load() != 21 {
		t.Fatalf("unsafe old orders retained: cancellations=%d", client.cancellations.Load())
	}
}
