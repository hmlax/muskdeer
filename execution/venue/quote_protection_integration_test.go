//go:build integration

package venue

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/logging"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func assertCurrentQuoteContinuity(ctx context.Context, t *testing.T, setup *currentStateTest) {
	t.Helper()
	runtime, profile, client := setup.runtime, setup.profile, setup.client
	profile.StrategyInstanceId = setup.instance.Id
	profile.MaximumOrderNotional = "100"
	runtime.cfg.Venue = &common.VenuePolicy{LeaseTtl: durationpb.New(10 * time.Second)}
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Update("maximum_notional", "10000").Error; err != nil {
		t.Fatal(err)
	}
	client.snapshot.Orders = nil
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		for level := range 20 {
			price := decimal.NewFromInt(100).Sub(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			if side == domain.SideSell {
				price = decimal.NewFromInt(100).Add(decimal.NewFromInt(int64(level + 1)).Shift(-1))
			}
			client.snapshot.Orders = append(client.snapshot.Orders, &common.VenueOrder{
				OrderId:          strconv.Itoa(1000 + len(client.snapshot.Orders)),
				ClientId:         "deer_BTCUSDT_s1_depth_" + strconv.Itoa(len(client.snapshot.Orders)),
				Side:             string(side),
				Price:            price.String(),
				Quantity:         "0.1",
				ExecutedQuantity: "0",
				Status:           native.StatusOpen,
			})
		}
	}
	book := &common.VenueBook{
		Symbol:       profile.Symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		Publisher:    native.Authority(setup.sourceLease),
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Bids:         []*common.VenueLevel{{Price: "99.9"}},
		Asks:         []*common.VenueLevel{{Price: "100.1"}},
	}
	runtime.rememberQuotePlan(profile, &execution.QuotePlan{Strategy: native.Authority(setup.strategyLease)}, book)
	// The database and SDK use real clocks. Cross the original frame's expiry
	// while the accepted strategy lease remains live, then supply a fresh frame.
	timer := time.NewTimer(time.Until(time.UnixMilli(book.ValidUntilMs + 20)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	fresh := func() quoteObservation {
		book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
		snapshot := proto.CloneOf(client.snapshot)
		snapshot.StartedAtMs = time.Now().UnixMilli()
		snapshot.ObservedAtMs = snapshot.StartedAtMs
		snapshot.ValidUntilMs = snapshot.StartedAtMs + native.SnapshotAge.Milliseconds()
		return quoteObservation{
			lease:    setup.lease,
			book:     proto.CloneOf(book),
			snapshot: snapshot,
		}
	}
	if err := runtime.protectObserved(ctx, profile, fresh()); err != nil || client.cancellations.Load() != 0 {
		t.Fatalf("protectObserved(fresh source, expired prior frame) cancelled=%d err=%v, want 40 safe quotes retained", client.cancellations.Load(), err)
	}
	assertVisibleReplacement(ctx, t, setup, fresh().book)
	beforeUnsafe := client.cancellations.Load()
	client.snapshot.Orders[0].Price = "101"
	if err := runtime.protectObserved(ctx, profile, fresh()); err != nil || client.cancellations.Load() != beforeUnsafe+1 {
		t.Fatalf("protectObserved(one newly crossed quote) cancelled=%d err=%v, want only 1", client.cancellations.Load(), err)
	}
	if err := setup.db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Where("account_id = ?", setup.strategyLease.Key.AccountID).UpdateColumn("epoch", setup.strategyLease.Epoch+1).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.protectObserved(ctx, profile, fresh()); err != nil || client.cancellations.Load() != beforeUnsafe+40 {
		t.Fatalf("protectObserved(strategy ownership lost) cancelled=%d err=%v, want all 40", client.cancellations.Load(), err)
	}
}

func assertVisibleReplacement(ctx context.Context, t *testing.T, setup *currentStateTest, book *common.VenueBook) {
	t.Helper()
	runtime, profile, client := setup.runtime, setup.profile, setup.client
	logger, err := logging.New(ctx, nil, logging.Identity{
		Service:  "execution",
		Instance: runtime.owner,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	runtime.logger = logger
	runtime.cfg.ReplacementFraction = decimal.RequireFromString("0.1")
	wanted := make(map[string]*SubmitRequest)
	var targets []*common.VenueTarget
	for _, order := range client.snapshot.Orders {
		price := decimal.RequireFromString(order.Price)
		wanted[order.Side+"/"+price.String()] = &SubmitRequest{
			Side:     domain.Side(order.Side),
			Price:    price,
			Quantity: decimal.RequireFromString("0.2"),
		}
		targets = append(targets, &common.VenueTarget{
			Side:     order.Side,
			Price:    order.Price,
			Quantity: "0.2",
		})
	}
	snapshot := proto.CloneOf(client.snapshot)
	retained, err := retainQuoteOrders(profile, snapshot, wanted, true, runtime.cfg.ReplacementFraction)
	if err != nil {
		t.Fatal(err)
	}
	client.minimumDepth = 20
	defer func() { client.minimumDepth = 0 }()
	result, err := runtime.maintainCurrentDepth(ctx, &quoteCycle{
		profile:     profile,
		bridgeSlots: 1,
		lease:       setup.lease,
		authority: LiveAuthority{
			Strategy:   native.Authority(setup.strategyLease),
			Book:       book,
			InstanceID: setup.instance.Id,
			Revision:   setup.instance.Revision,
		},
		targets:  quoteMaintenanceOrder(targets, snapshot),
		snapshot: snapshot,
		retained: retained,
		result:   &execution.QuoteResult{},
	}, wanted, true)
	if err != nil || result.GetPlaced() != 2 || result.GetCancelled() != 2 {
		t.Fatalf("maintainCurrentDepth(size refresh) placed=%d cancelled=%d err=%v, want 2/2 without any level gap", result.GetPlaced(), result.GetCancelled(), err)
	}
}
