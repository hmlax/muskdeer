//go:build integration

package venue

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type directQuoteClient struct{ *currentStateClient }

func (c *directQuoteClient) SubmitQuote(_ context.Context, request gateway.QuoteRequest) (*gateway.SubmitResult, error) {
	id := 9000 + c.submissions.Add(1)
	c.snapshot.Orders = append(c.snapshot.Orders, &common.VenueOrder{
		OrderId:          strconv.FormatUint(id, 10),
		ClientId:         request.ClientID,
		Side:             string(request.Side),
		Price:            request.Price.String(),
		Quantity:         request.Quantity.String(),
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	})
	return &gateway.SubmitResult{
		OrderID:  id,
		ClientID: request.ClientID,
	}, nil
}

func (c *directQuoteClient) CancelQuote(_ context.Context, request gateway.CancelRequest) error {
	c.cancellations.Add(1)
	for _, order := range c.snapshot.Orders {
		if order.OrderId == strconv.FormatUint(request.OrderID, 10) {
			order.Status = native.StatusCanceled
		}
	}
	return nil
}

func TestMakerQuoteNeverReadsOrWritesDatabaseOrders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	runtime := setup.runtime
	runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	runtime.store.matching = true
	setup.profile.StrategyInstanceId = setup.instance.Id
	setup.profile.MaximumOrderNotional = "100"
	client := &directQuoteClient{currentStateClient: setup.client}
	runtime.client.Store(&gatewaySession{Client: client})
	before, err := runtime.store.Budget(ctx, setup.budget.AccountId)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := denyMakerOrderStorage(t, setup.db)
	book := &common.VenueBook{
		Symbol:       setup.profile.Symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Publisher:    &common.VenueAuthority{AccountId: native.MarketKey(setup.profile.Symbol)},
		Bids:         []*common.VenueLevel{{Price: "99"}},
		Asks:         []*common.VenueLevel{{Price: "101"}},
	}
	lease := domain.LeaseToken{Key: domain.LeaseKey{AccountID: setup.budget.AccountId}}
	snapshot, err := runtime.refresh(ctx, setup.profile, lease, book)
	if err != nil {
		t.Fatal(err)
	}
	cycle := &quoteCycle{
		profile:  setup.profile,
		lease:    lease,
		snapshot: snapshot,
		authority: LiveAuthority{
			InstanceID: setup.instance.Id,
			Revision:   setup.instance.Revision,
			Book:       book,
			Strategy:   &common.VenueAuthority{AccountId: native.StrategyKey(10001, 10001)},
		},
		result: &execution.QuoteResult{},
	}
	if err := runtime.submitQuote(ctx, cycle, &common.VenueTarget{
		Side:     string(domain.SideBuy),
		Price:    "99",
		Quantity: "1",
	}); err != nil {
		t.Fatalf("direct maker placement accessed storage or failed: %v", err)
	}
	snapshot, err = runtime.refresh(ctx, setup.profile, lease, book)
	if err != nil || len(snapshot.GetOrders()) != 1 {
		t.Fatalf("live quote observation failed: %v", err)
	}
	runtime.rememberQuotePlan(setup.profile, &execution.QuotePlan{}, book)
	if err := runtime.protectObserved(ctx, setup.profile, quoteObservation{
		lease:    lease,
		book:     book,
		snapshot: snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.cancelManaged(ctx, setup.profile); err != nil {
		t.Fatal(err)
	}
	after, err := runtime.store.Budget(ctx, setup.budget.AccountId)
	if err != nil {
		t.Fatal(err)
	}
	if client.submissions.Load() != 1 || client.cancellations.Load() != 1 || cycle.result.Placed != 1 {
		t.Fatalf("direct SDK operations submit=%d cancel=%d", client.submissions.Load(), client.cancellations.Load())
	}
	if !decimal.RequireFromString(before.ObservedNotional).Equal(decimal.RequireFromString(after.ObservedNotional)) ||
		!decimal.RequireFromString(before.ReservedNotional).Equal(decimal.RequireFromString(after.ReservedNotional)) || before.UpdateAt != after.UpdateAt {
		t.Fatal("maker persisted runtime account/order observations")
	}
	if forbidden.Load() != 0 {
		t.Fatalf("maker order-storage accesses=%d", forbidden.Load())
	}
	assertDirectQuoteControls(ctx, t, setup, cycle)
}

func assertDirectQuoteControls(ctx context.Context, t *testing.T, setup *currentStateTest, cycle *quoteCycle) {
	t.Helper()
	cycle.snapshot = setup.snapshot
	cycle.admitted = nil
	cycle.authority.Book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	cycle.snapshot.StartedAtMs = time.Now().UnixMilli()
	cycle.snapshot.ValidUntilMs = cycle.snapshot.StartedAtMs + native.SnapshotAge.Milliseconds()
	if err := setup.runtime.submitQuote(ctx, cycle, &common.VenueTarget{
		Side:     string(domain.SideBuy),
		Price:    "101",
		Quantity: "1",
	}); !errors.Is(err, ErrRiskLimit) {
		t.Fatalf("oversized direct quote = %v", err)
	}
	if _, err := instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStop,
		Subject:                "maker-direct-test",
		RequestID:              "maker-direct-test-stop",
		ID:                     setup.instance.Id,
		ExpectedRevision:       setup.instance.Revision,
		ExpectedControlVersion: setup.instance.RequestedConfigVersion,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := setup.runtime.submitQuote(ctx, cycle, &common.VenueTarget{
		Side:     string(domain.SideBuy),
		Price:    "99",
		Quantity: "1",
	}); err == nil {
		t.Fatal("stopped maker instance placed a direct order")
	}
	if setup.client.submissions.Load() != 1 {
		t.Fatal("rejected maker request reached SDK")
	}
}

func denyMakerOrderStorage(t *testing.T, db *gorm.DB) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	deny := func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		switch tx.Statement.Schema.Table {
		case "venue_order_intents", "venue_cancellations", "venue_account_snapshots", "execution_shard_leases":
			calls.Add(1)
			tx.Error = errors.Join(tx.Error, errors.New("maker touched persisted order state"))
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("deny_maker_order_state", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("deny_maker_order_state", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("deny_maker_order_state", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("deny_maker_order_state", deny); err != nil {
		t.Fatal(err)
	}
	return &calls
}
