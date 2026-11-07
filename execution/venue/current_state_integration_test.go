//go:build integration

package venue

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	"github.com/hmlax/muskdeer/deer/ownerid"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/adapters/himalaya"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// The embedded interface deliberately fails if quotation tries a history RPC.
type currentStateClient struct {
	gateway.Client
	snapshot      *common.VenueSnapshot
	store         *Store
	cancellations atomic.Int64
	submissions   atomic.Uint64
	minimumDepth  int
}

func (c *currentStateClient) Read(context.Context, gateway.Scope) (*gateway.Observation, error) {
	snapshot := proto.CloneOf(c.snapshot)
	snapshot.StartedAtMs = time.Now().UnixMilli()
	snapshot.ObservedAtMs = snapshot.StartedAtMs
	snapshot.ValidUntilMs = snapshot.StartedAtMs + native.SnapshotAge.Milliseconds()
	return &gateway.Observation{Snapshot: snapshot}, nil
}

func (c *currentStateClient) ReadOrders(context.Context, gateway.Scope) ([]*common.VenueOrder, error) {
	return proto.CloneOf(c.snapshot).Orders, nil
}

func (c *currentStateClient) Cancel(ctx context.Context, admission gateway.CancelAdmission) error {
	return c.store.WithCancellation(ctx, admission, func(context.Context) error {
		c.cancellations.Add(1)
		for _, order := range c.snapshot.Orders {
			if order.OrderId == strconv.FormatUint(admission.Request.OrderID, 10) {
				order.Status = native.StatusCanceled
			}
		}
		if c.minimumDepth > 0 {
			for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
				levels := make(map[string]bool)
				for _, order := range c.snapshot.Orders {
					if order.Side == string(side) && native.Active(order.Status) {
						levels[order.Price] = true
					}
				}
				if len(levels) < c.minimumDepth {
					return errors.New("replacement removed a live price level before another existed")
				}
			}
		}
		return nil
	})
}

func (c *currentStateClient) Submit(ctx context.Context, request gateway.SubmitRequest) (*gateway.SubmitResult, error) {
	digest, err := c.store.gateway.RequestDigest(request)
	if err != nil {
		return nil, err
	}
	var id uint64
	err = c.store.WithSubmission(ctx, gateway.Admission{
		IntentID: request.IntentID,
		OwnerID:  request.OwnerID,
		Epoch:    request.Epoch,
		UserID:   request.UserID,
		FundID:   request.FundID,
		ClientID: request.ClientID,
		Digest:   digest,
	}, func(context.Context) error {
		id = 5000 + c.submissions.Add(1)
		c.snapshot.Orders = append(c.snapshot.Orders, &common.VenueOrder{
			OrderId:          strconv.FormatUint(id, 10),
			ClientId:         request.ClientID,
			Side:             string(request.Side),
			Price:            request.Price.String(),
			Quantity:         request.Quantity.String(),
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		})
		return nil
	})
	return &gateway.SubmitResult{
		OrderID:  id,
		ClientID: request.ClientID,
	}, err
}

type currentStateTest struct {
	db            *gorm.DB
	store         *Store
	lease         domain.LeaseToken
	strategyLease domain.LeaseToken
	sourceLease   domain.LeaseToken
	budget        *model.TradingAccountPolicy
	instance      *strategy.StrategyInstance
	old           *execution.VenueOrderIntent
	known         *execution.VenueOrderIntent
	client        *currentStateClient
	runtime       *Runtime
	profile       *common.VenueProfilePolicy
	snapshot      *common.VenueSnapshot
}

func newCurrentStateTest(ctx context.Context, t *testing.T) *currentStateTest {
	t.Helper()
	database := os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE")
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || !strings.HasPrefix(database, "muskdeer_test_") {
		t.Fatal("isolated PostgreSQL test database required")
	}
	connection, err := postgres.New(ctx, &config.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         database,
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           "public",
		MaxOpenConns:     4,
		MaxIdleConns:     2,
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(5 * time.Second),
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(time.Minute),
	}, "current-state-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	db := connection.GORM()
	if err := schemaformat.Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := AccountID(10001, 10001)
	if err := db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Create(&model.ExecutionShardLease{AccountId: account}).Error; err != nil {
		t.Fatal(err)
	}
	budget := &model.TradingAccountPolicy{
		AccountId:            account,
		UserId:               10001,
		FundId:               10001,
		Symbol:               "BTCUSDT",
		MarketDataSource:     native.Source,
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
		Leverage:             10,
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		ObservedNotional:     "9000",
		ReservedNotional:     "100000",
		AvailableQuote:       "1000",
		WorstFeeRate:         "0.001",
		Enabled:              true,
		CutoverCompleted:     true,
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(budget).Error; err != nil {
		t.Fatal(err)
	}
	instrument := &model.Instrument{
		ExecutionVenue:  native.Venue,
		Symbol:          budget.Symbol,
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		MinNotional:     "1",
		Status:          domain.InstrumentStatusTrading,
		MetadataVersion: 1,
	}
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := &strategy.StrategyInstance{
		InstanceKey:            "current-state-maker",
		Template:               string(domain.StrategyTemplateMarketMaker),
		Implementation:         strategyregistry.ImplementationMaker,
		TemplateId:             strategyregistry.MakerTemplateID,
		TemplateRevision:       1,
		AccountId:              account,
		InstrumentId:           instrument.Id,
		MarketDataSource:       native.Source,
		CodeVersion:            "test",
		Revision:               1,
		State:                  string(domain.StrategyStateCreated),
		DesiredRunning:         true,
		DesiredState:           string(domain.StrategyStateRunning),
		RequestedConfigVersion: 1,
		Parameters: &common.StrategyInstanceParameters{
			SchemaVersion: 1,
			Execution: &common.StrategyExecutionParameters{
				PollIntervalMs:     200,
				OperationTimeoutMs: 2000,
				LeaseTtlMs:         10000,
				Leverage:           10,
			},
			MarketMaker: &common.DepthMakerParameters{
				Depth:               20,
				QuantityFraction:    "0.1",
				ReplacementFraction: "0.1",
				BridgeSlots:         1,
			},
		},
	}
	if err := instanceconfig.Import(ctx, db, &common.StrategyInstanceImport{Instances: []*common.StrategyInstanceInput{{
		InstanceKey:      instance.InstanceKey,
		TemplateId:       instance.TemplateId,
		TemplateRevision: instance.TemplateRevision,
		AccountId:        instance.AccountId,
		InstrumentId:     instance.InstrumentId,
		MarketDataSource: instance.MarketDataSource,
		DesiredRunning:   true,
		Parameters:       instance.Parameters,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", instance.InstanceKey).First(instance).Error; err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "current-state")
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(key string) domain.LeaseToken {
		t.Helper()
		token, err := leases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: key,
		}, owner, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	lease := acquire(account)
	strategyLease := acquire(native.StrategyKey(10001, 10001))
	sourceLease := acquire(native.MarketKey(budget.Symbol))
	old := &execution.VenueOrderIntent{
		AccountId:        account,
		UserId:           10001,
		FundId:           10001,
		Symbol:           budget.Symbol,
		ClientId:         "lost-old-reply",
		OwnerId:          owner,
		OwnerEpoch:       lease.Epoch,
		RequestDigest:    strings.Repeat("a", 64),
		Side:             string(domain.SideBuy),
		Price:            "100",
		Quantity:         "1",
		Leverage:         10,
		State:            IntentDispatched,
		ReservedNotional: "100",
	}
	if err := db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Create(old).Error; err != nil {
		t.Fatal(err)
	}
	known := proto.CloneOf(old)
	known.Id = 0
	known.CreateAt, known.UpdateAt = time.Now().Add(-time.Minute).UnixMilli(), time.Now().Add(-time.Minute).UnixMilli()
	known.State, known.OrderId, known.ClientId = IntentAcknowledged, "321", "old-known-order"
	known.Price, known.Quantity, known.ReservedNotional = "1000", "10", "10000"
	if err := db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Create(known).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db, himalaya.Factory{})
	if err != nil {
		t.Fatal(err)
	}
	cancellation, err := store.PrepareCancel(ctx, lease, CancelRequest{
		Scope: Scope{
			UserID: 10001,
			FundID: 10001,
			Symbol: budget.Symbol,
		},
		OrderID: 123,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithCancellation(ctx, cancellation, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	snapshot := &common.VenueSnapshot{
		Scope: &common.VenueScope{
			UserId: 10001,
			FundId: 10001,
			Symbol: budget.Symbol,
		},
		Market: &common.VenueMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MaximumLeverage: 10,
			MarkPrice:       "10",
			WorstFeeRate:    "0.001",
		},
		Positions: []*common.VenuePosition{{
			Symbol:         budget.Symbol,
			SignedQuantity: "1",
		}},
		Available:    "1000",
		StartedAtMs:  now,
		ObservedAtMs: now,
		ValidUntilMs: now + native.SnapshotAge.Milliseconds(),
	}
	client := &currentStateClient{
		snapshot: snapshot,
		store:    store,
	}
	runtime := &Runtime{
		db:     db,
		store:  store,
		owner:  owner,
		leases: leases,
		plans:  map[string]*atomic.Int64{account: {}},
		cfg: &venueconfig.RuntimeConfig{
			InstanceID: instance.Id,
			Revision:   instance.Revision,
		},
		client: &atomic.Pointer[gatewaySession]{},
	}
	runtime.client.Store(&gatewaySession{Client: client})
	profile := &common.VenueProfilePolicy{
		UserId:       10001,
		FundId:       10001,
		Symbol:       budget.Symbol,
		PriceTick:    "0.1",
		QuantityStep: "0.001",
		Leverage:     10,
	}
	return &currentStateTest{
		db:            db,
		store:         store,
		lease:         lease,
		strategyLease: strategyLease,
		sourceLease:   sourceLease,
		budget:        budget,
		instance:      instance,
		old:           old,
		known:         known,
		client:        client,
		runtime:       runtime,
		profile:       profile,
		snapshot:      snapshot,
	}
}

func TestCurrentStateQuotesWithoutHistoricalProofAndKeepsUnknownAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	db, store, lease := setup.db, setup.store, setup.lease
	strategyLease, sourceLease := setup.strategyLease, setup.sourceLease
	budget, instance := setup.budget, setup.instance
	account := budget.AccountId
	old, known := setup.old, setup.known
	client, runtime, profile, snapshot := setup.client, setup.runtime, setup.profile, setup.snapshot
	if _, err := runtime.refresh(ctx, profile, lease, nil); err != nil {
		t.Fatalf("refresh(current snapshot, unknown history) = %v, want current state", err)
	}
	if pending, err := runtime.unresolvedCancels(ctx, account); err != nil || pending {
		t.Fatalf("unresolvedCancels(current instance) = %t, %v, want no history gate", pending, err)
	}
	authority := LiveAuthority{
		Strategy: native.Authority(strategyLease),
		Book: &common.VenueBook{
			Source:       native.Source,
			Segment:      native.Segment,
			Symbol:       budget.Symbol,
			Publisher:    native.Authority(sourceLease),
			ValidUntilMs: snapshot.ValidUntilMs,
		},
		InstanceID: instance.Id,
		Revision:   instance.Revision,
	}
	request := SubmitRequest{
		UserID:   10001,
		FundID:   10001,
		Symbol:   budget.Symbol,
		ClientID: "fresh-current-state",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
		Leverage: 10,
	}
	prepared, err := store.PrepareLive(ctx, lease, request, authority)
	if err != nil {
		t.Fatalf("PrepareLive(current state, old unknown) = %v, want new admission", err)
	}
	calls := 0
	for attempt := range 2 {
		err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil })
		if attempt == 0 && err != nil || attempt == 1 && !errors.Is(err, domain.ErrOutcomeUnknown) {
			t.Fatalf("WithSubmission(attempt %d) = %v", attempt, err)
		}
	}
	if calls != 1 {
		t.Fatalf("WithSubmission(replay) calls = %d, want 1", calls)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", budget.Id).First(budget).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(budget.ObservedNotional).Equal(decimal.NewFromInt(10)) || !decimal.RequireFromString(budget.ReservedNotional).Equal(decimal.NewFromInt(100100)) {
		t.Fatalf("current exposure/audit reserve = %s/%s, want 10/100100", budget.ObservedNotional, budget.ReservedNotional)
	}
	var preserved execution.VenueOrderIntent
	if err := db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", old.Id).First(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.State != IntentDispatched || preserved.OrderId != "" || preserved.HistoricalApprovalReference != "" || !decimal.RequireFromString(preserved.ReservedNotional).Equal(decimal.NewFromInt(100)) {
		t.Fatal("current-state admission changed the historical unknown fact or reservation")
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", budget.Id).Update("maximum_notional", "250").Error; err != nil {
		t.Fatal(err)
	}
	request.ClientID += "-budget-rejected"
	if _, err := store.PrepareLive(ctx, lease, request, authority); !errors.Is(err, ErrRiskLimit) {
		t.Fatalf("PrepareLive(unobserved pending obligations exceed budget) = %v, want risk limit", err)
	}
	client.snapshot.Orders = []*common.VenueOrder{{
		OrderId:          known.OrderId,
		ClientId:         known.ClientId,
		Side:             known.Side,
		Price:            known.Price,
		Quantity:         known.Quantity,
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	}}
	if _, err := runtime.refresh(ctx, profile, lease, nil); err != nil {
		t.Fatalf("refresh(current active order) = %v, want native exposure", err)
	}
	budget, err = store.Budget(ctx, account)
	if err != nil || !decimal.RequireFromString(budget.ObservedNotional).Equal(decimal.NewFromInt(10010)) {
		t.Fatalf("current native position plus active order = %s, %v, want 10010", budget.GetObservedNotional(), err)
	}
	var unchanged execution.VenueOrderIntent
	if err := db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", known.Id).First(&unchanged).Error; err != nil || unchanged.UpdateAt != known.UpdateAt {
		t.Fatalf("reobserving active order changed its acknowledgement time: %d, %v", unchanged.UpdateAt, err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", budget.Id).Update("snapshot_expires_at", time.Now().UnixMilli()-1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareLive(ctx, lease, request, authority); !errors.Is(err, ErrRiskLimit) {
		t.Fatalf("PrepareLive(expired current snapshot) = %v, want risk limit", err)
	}
	assertCurrentQuoteContinuity(ctx, t, setup)
}
