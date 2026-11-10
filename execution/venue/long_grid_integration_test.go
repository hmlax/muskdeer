//go:build integration

package venue

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/logging"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
)

type longGridTestClient struct {
	gateway.Client
	mu              sync.Mutex
	snapshot        *common.VenueSnapshot
	requests        []gateway.GridRequest
	results         map[string]*gateway.GridOrder
	lostReply       bool
	hideHistory     bool
	partialQuantity string
}

func (c *longGridTestClient) ReadGrid(_ context.Context, _ gateway.Scope) (*gateway.Observation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	view := proto.CloneOf(c.snapshot)
	view.StartedAtMs = time.Now().UnixMilli()
	view.ObservedAtMs = view.StartedAtMs
	view.ValidUntilMs = view.StartedAtMs + native.SnapshotAge.Milliseconds()
	return &gateway.Observation{
		Snapshot:      view,
		BasePrecision: 3,
	}, nil
}
func (c *longGridTestClient) SubmitGrid(_ context.Context, in gateway.GridRequest) (*gateway.SubmitResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, in)
	price := in.Price
	if in.Market {
		price = decimal.RequireFromString(c.snapshot.Market.BestBid)
	}
	filled := in.Quantity
	if c.partialQuantity != "" {
		filled = decimal.RequireFromString(c.partialQuantity)
		c.partialQuantity = ""
	}
	orderStatus := native.StatusFilled
	if filled.LessThan(in.Quantity) {
		orderStatus = native.StatusCanceled
	}
	id := uint64(9000 + len(c.requests))
	c.results[in.ClientID] = &gateway.GridOrder{
		OrderID:          id,
		ClientID:         in.ClientID,
		Side:             in.Side,
		ReduceOnly:       in.ReduceOnly,
		Quantity:         in.Quantity.String(),
		ExecutedQuantity: filled.String(),
		ExecutedNotional: price.Mul(filled).String(),
		Status:           orderStatus,
	}
	old, entry := decimal.Zero, decimal.Zero
	if len(c.snapshot.Positions) > 0 {
		old = decimal.RequireFromString(c.snapshot.Positions[0].SignedQuantity)
		entry = decimal.RequireFromString(c.snapshot.Positions[0].EntryPrice)
	}
	if in.Side == domain.SideBuy {
		total := old.Add(filled)
		average := old.Mul(entry).Add(filled.Mul(price)).DivRound(total, 18)
		c.snapshot.Positions = []*common.VenuePosition{{
			Symbol:         in.Scope.Symbol,
			PositionId:     "actual-long-position",
			SignedQuantity: total.String(),
			EntryPrice:     average.String(),
			Leverage:       "10",
		}}
	} else {
		if !in.ReduceOnly || in.PositionID != "actual-long-position" {
			return nil, longgrid.ErrInvalid
		}
		left := old.Sub(filled)
		if left.IsZero() {
			c.snapshot.Positions = nil
		} else {
			c.snapshot.Positions[0].SignedQuantity = left.String()
		}
	}
	if c.lostReply {
		c.lostReply = false
		return nil, context.DeadlineExceeded
	}
	return &gateway.SubmitResult{
		OrderID:  id,
		ClientID: in.ClientID,
	}, nil
}
func (c *longGridTestClient) FindGridOrder(_ context.Context, _ gateway.Scope, client string, _ uint64, _ int32) (*gateway.GridOrder, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hideHistory || c.results[client] == nil {
		return nil, domain.ErrOutcomeUnknown
	}
	result := *c.results[client]
	return &result, nil
}
func (c *longGridTestClient) SubmitQuote(context.Context, gateway.QuoteRequest) (*gateway.SubmitResult, error) {
	return nil, longgrid.ErrInvalid
}
func (c *longGridTestClient) CancelQuote(context.Context, gateway.CancelRequest) error { return nil }
func (c *longGridTestClient) setPrice(bid, ask, mark string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.Market.BestBid = bid
	c.snapshot.Market.BestAsk = ask
	c.snapshot.Market.MarkPrice = mark
}
func (c *longGridTestClient) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.requests) }

type longGridFixture struct {
	setup  *currentStateTest
	row    *strategy.StrategyInstance
	cfg    *venueconfig.RuntimeConfig
	logger *logging.Logger
	client *longGridTestClient
}

func newLongGridFixture(ctx context.Context, t *testing.T) *longGridFixture {
	t.Helper()
	setup := newCurrentStateTest(ctx, t)
	row, err := instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Subject:   "long-grid-test",
		RequestID: "create-long-grid-test",
		Input: &common.StrategyInstanceInput{
			InstanceKey:      "long-grid-test",
			TemplateId:       strategyregistry.LongGridTemplateID,
			TemplateRevision: 1,
			AccountId:        setup.instance.AccountId,
			InstrumentId:     setup.instance.InstrumentId,
			MarketDataSource: native.Source,
			Parameters: &common.StrategyInstanceParameters{
				SchemaVersion: 1,
				Execution:     proto.CloneOf(setup.instance.Parameters.Execution),
				LongGrid: &common.LongGridParameters{
					SpacingFraction:         "0.01",
					StopLossFraction:        "0.005",
					OrderNotional:           "100",
					MaximumPositionNotional: "1000",
					OpenOnStart:             true,
					HaltAfterStopLoss:       true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                "long-grid-test",
		RequestID:              "start-long-grid-test",
		ID:                     row.Id,
		ExpectedRevision:       row.Revision,
		ExpectedControlVersion: row.RequestedConfigVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	instances, err := instanceconfig.LoadConfigured(ctx, setup.db)
	if err != nil {
		t.Fatal(err)
	}
	var cfg *venueconfig.RuntimeConfig
	for _, instance := range instances {
		if instance.ID == row.Id {
			cfg, err = instance.Settings()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if cfg == nil {
		t.Fatal("long grid not loaded")
	}
	logger, err := logging.New(ctx, nil, logging.Identity{
		Service:  "execution",
		Instance: "long-grid-test",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	return &longGridFixture{
		setup:  setup,
		row:    row,
		cfg:    cfg,
		logger: logger,
		client: &longGridTestClient{
			snapshot: &common.VenueSnapshot{
				Scope:     native.Scope(cfg.Venue.Profiles[0]),
				Available: "10000",
				Market: &common.VenueMarket{
					PriceTick:       "0.1",
					QuantityStep:    "0.001",
					MinimumQuantity: "0.001",
					MinimumNotional: "1",
					MaximumQuantity: "100",
					MaximumLeverage: 10,
					WorstFeeRate:    "0.001",
					MarkPrice:       "100",
					BestBid:         "99.9",
					BestAsk:         "100",
				},
			},
			results: make(map[string]*gateway.GridOrder),
		},
	}
}
func (f *longGridFixture) runtime() *Runtime {
	client := &atomic.Pointer[gatewaySession]{}
	client.Store(&gatewaySession{Client: f.client})
	return &Runtime{
		db:      f.setup.db,
		cfg:     f.cfg,
		logger:  f.logger,
		client:  client,
		closing: &atomic.Bool{},
		gates:   map[string]chan struct{}{f.setup.budget.AccountId: make(chan struct{}, 1)},
	}
}
func (f *longGridFixture) book() *common.VenueBook {
	f.client.mu.Lock()
	defer f.client.mu.Unlock()
	return &common.VenueBook{
		Symbol:       "BTCUSDT",
		Source:       native.Source,
		Segment:      native.Segment,
		Sequence:     1,
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Publisher:    &common.VenueAuthority{AccountId: native.MarketKey("BTCUSDT")},
		Bids:         []*common.VenueLevel{{Price: f.client.snapshot.Market.BestBid}},
		Asks:         []*common.VenueLevel{{Price: f.client.snapshot.Market.BestAsk}},
	}
}
func TestLongGridExecutesFirstBuyProfitBuybackAndIndependentFullStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newLongGridFixture(ctx, t)
	runtime := fixture.runtime()
	evaluate := func(open bool) {
		t.Helper()
		var book *common.VenueBook
		if open {
			book = fixture.book()
		}
		if _, err := runtime.evaluateLongGrid(ctx, book, open); err != nil {
			t.Fatal(err)
		}
	}
	evaluate(true)
	evaluate(true)
	if fixture.client.count() != 1 {
		t.Fatal("initial position opened repeatedly")
	}
	fixture.client.setPrice("101", "101.1", "101")
	evaluate(true)
	evaluate(true)
	if fixture.client.count() != 2 || !fixture.client.requests[1].ReduceOnly || fixture.client.requests[1].Quantity.String() != "1" {
		t.Fatal("profit did not close corresponding lot")
	}
	evaluate(true)
	if fixture.client.count() != 2 {
		t.Fatal("flat grid immediately reopened above buyback")
	}
	fixture.client.setPrice("99.9", "100", "100")
	evaluate(true)
	evaluate(true)
	if fixture.client.count() != 3 {
		t.Fatal("one percent buyback missing")
	}
	fixture.client.setPrice("99.5", "99.6", "99.5")
	evaluate(false)
	evaluate(false)
	if fixture.client.count() != 4 || !fixture.client.requests[3].ReduceOnly || !fixture.client.requests[3].Market || fixture.client.requests[3].Quantity.String() != "1" {
		t.Fatal("stop failed to reduce full position")
	}
	var state execution.LongGridRuntime
	if err := fixture.setup.db.WithContext(ctx).Model(&execution.LongGridRuntime{}).Where("instance_id = ?", fixture.row.Id).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.State != longgrid.Stopped || state.PendingActionId != nil || state.StopActionId != nil || len(state.GetLots().GetItems()) != 0 {
		t.Fatalf("stop cursor = %v", &state)
	}
	var row strategy.StrategyInstance
	if err := fixture.setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", fixture.row.Id).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.DesiredRunning {
		t.Fatal("stop loss left strategy running")
	}
	evaluate(true)
	if fixture.client.count() != 4 {
		t.Fatal("stopped grid automatically reopened")
	}
	restarted, err := instanceconfig.ManageConfigured(ctx, fixture.setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                "long-grid-test",
		RequestID:              "explicit-grid-restart",
		ID:                     row.Id,
		ExpectedRevision:       row.Revision,
		ExpectedControlVersion: row.RequestedConfigVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.DesiredRunning {
		t.Fatal("explicit restart failed")
	}
	evaluate(true)
	if fixture.client.count() != 5 {
		t.Fatal("explicit restart did not open a new episode")
	}
}
func TestLongGridLostReplySurvivesRestartAndDoesNotBlockRiskReduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newLongGridFixture(ctx, t)
	fixture.client.lostReply = true
	fixture.client.hideHistory = true
	runtime := fixture.runtime()
	if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err != nil {
		t.Fatal(err)
	}
	runtime = fixture.runtime() // New process memory, same durable cursor and venue.
	if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 1 {
		t.Fatal("UNKNOWN opening request was replayed")
	}
	fixture.client.setPrice("99.4", "99.5", "99.4")
	if _, err := runtime.evaluateLongGrid(ctx, nil, false); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 2 || !fixture.client.requests[1].ReduceOnly {
		t.Fatal("UNKNOWN opening blocked full close")
	}
	fixture.client.hideHistory = false
	if _, err := runtime.evaluateLongGrid(ctx, nil, false); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 2 {
		t.Fatal("resolved old action was sent again")
	}
}
func TestLongGridSeparateExecutorsCannotDuplicateFirstOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newLongGridFixture(ctx, t)
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		runtime := fixture.runtime()
		go func() { <-start; _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); done <- err }()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil && !errors.Is(err, domain.ErrLeaseHeld) {
			t.Fatal(err)
		}
	}
	if fixture.client.count() != 1 {
		t.Fatalf("duplicate initial SDK orders: %s", strconv.Itoa(fixture.client.count()))
	}
}

func TestLongGridTracksPartialFillsAndReducesOnlyActualLot(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newLongGridFixture(ctx, t)
	runtime := fixture.runtime()
	fixture.client.partialQuantity = "0.4"
	for range 2 {
		if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err != nil {
			t.Fatal(err)
		}
	}
	fixture.client.setPrice("101", "101.1", "101")
	if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 2 || fixture.client.requests[1].Quantity.String() != "0.4" || !fixture.client.requests[1].ReduceOnly {
		t.Fatal("partial fill produced an oversized sell")
	}
}

func TestLongGridStaleSourcePausedAndLiveBudgetBlockOpening(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newLongGridFixture(ctx, t)
	runtime := fixture.runtime()
	stale := fixture.book()
	stale.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
	if _, err := runtime.evaluateLongGrid(ctx, stale, true); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 0 {
		t.Fatal("stale source opened risk")
	}
	if err := fixture.setup.db.WithContext(ctx).Model(fixture.setup.budget).Where("id = ?", fixture.setup.budget.Id).Update("MaximumOrderNotional", "50").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err == nil {
		t.Fatal("live order limit was ignored")
	}
	if fixture.client.count() != 0 {
		t.Fatal("budget-rejected request reached SDK")
	}
	_, err := instanceconfig.ManageConfigured(ctx, fixture.setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStop,
		Subject:                "long-grid-test",
		RequestID:              "pause-before-grid-order",
		ID:                     fixture.row.Id,
		ExpectedRevision:       fixture.row.Revision,
		ExpectedControlVersion: fixture.row.RequestedConfigVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.evaluateLongGrid(ctx, fixture.book(), true); err != nil {
		t.Fatal(err)
	}
	if fixture.client.count() != 0 {
		t.Fatal("paused instance opened")
	}
}

func TestLongGridCanBindAnotherFundToAnExistingMakerSymbol(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	policy := proto.CloneOf(setup.budget)
	policy.Id = 0
	policy.CreateAt = 0
	policy.UpdateAt = 0
	policy.UserId = 20000
	policy.FundId = 20000
	policy.AccountId = native.ScopeKey(20000, 20000)
	policy.ObservedNotional = "0"
	policy.ReservedNotional = "0"
	if err := setup.db.WithContext(ctx).Model(policy).Create(policy).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := setup.db.WithContext(ctx).Model(policy).Where("symbol = ?", policy.Symbol).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("new grid fund replaced the maker binding")
	}
}
