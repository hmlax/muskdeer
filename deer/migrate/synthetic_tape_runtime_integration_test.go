//go:build integration

package migrate

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	rpc "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/testcert"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
	nativestrategy "github.com/hmlax/muskdeer/strategy/native"
)

func TestSyntheticTapeWorkersUseSDKWithoutOrdersAndDoNotResetOnRestart(t *testing.T) {
	testSyntheticTapeWorkers(t, configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK)
}

func TestSyntheticTradeWorkersFollowLatestSourceWithoutFallbackOrOrders(t *testing.T) {
	testSyntheticTapeWorkers(t, configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE)
}

func testSyntheticTapeWorkers(t *testing.T, mode configv1.SyntheticTapeMode) {
	t.Helper()
	db, _, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	t.Cleanup(cancel)
	authority := liveAuthorityFixture(ctx, t, db, lease, request.Symbol)
	configureNativeProfileFixture(ctx, t, db, request)
	sdk := &nativeSDKFixture{orders: make(map[uint64]*pb.OrderRow)}
	address := startNativeSDK(t, sdk)
	ca := testcert.New(t)
	discovery := &nativeDiscoveryFixture{entries: make(map[string]nacos.Instance)}
	sourceLeases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	market := &nativeMarketFixture{
		authority: authority.Book.Publisher,
		leases:    sourceLeases,
		lease: domain.LeaseToken{
			Key: domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: authority.Book.Publisher.AccountId,
			},
			OwnerID:   authority.Book.Publisher.OwnerId,
			Epoch:     authority.Book.Publisher.Epoch,
			ExpiresAt: time.UnixMilli(authority.Book.Publisher.ExpiresAtMs),
		},
	}
	if mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		token, err := sourceLeases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: native.TradeKey(request.Symbol),
		}, lease.OwnerID, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		market.tradeAuthority = native.Authority(token)
		market.tradeLease = token
		market.tradeUnavailable.Store(true)
	}
	discovery.entries[rpcsecurity.ServiceMarket] = nacos.Instance{
		ID:      "market-test",
		Service: rpcsecurity.ServiceMarket,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceMarket, func(server *grpc.Server) { rpc.RegisterMarketServer(server, market) }),
	}
	logger := nativeWorkerLogger(ctx, t, lease.OwnerID)
	t.Setenv("SYNTHETIC_TEST_TOKEN", "test-token")
	cfg := &configv1.BusinessRuntime{
		Enabled:          true,
		ExecutionVenue:   native.Venue,
		PollInterval:     durationpb.New(100 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Himalaya: &configv1.HimalayaRuntime{
			Target:           address,
			TokenEnv:         "SYNTHETIC_TEST_TOKEN",
			Depth:            1,
			QuantityFraction: "0.1",
			LeaseTtl:         durationpb.New(10 * time.Second),
			SyntheticTape: &configv1.SyntheticTape{
				Mode:            mode,
				Enabled:         true,
				DevelopmentOnly: true,
				RunId:           "test-tape-runtime",
				Interval:        durationpb.New(100 * time.Millisecond),
				MaximumEvents:   2,
				Profiles: []*configv1.SyntheticTapeProfile{{
					Symbol:          request.Symbol,
					Quantity:        "0.1",
					MaximumNotional: "100",
				}},
			},
		},
	}
	execution, err := himalaya.NewRuntime(ctx, db, lease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceExecution, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	discovery.entries[rpcsecurity.ServiceExecution] = nacos.Instance{
		ID:      "execution-sdk-test",
		Service: rpcsecurity.ServiceExecution,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceExecution, func(server *grpc.Server) { execution.RegisterGRPC(server) }),
	}
	unauthorized := nativeFactory(ctx, t, ca, rpcsecurity.ServicePortfolio, discovery)
	connection, err := unauthorized.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.NewExecutionClient(connection).PublishSynthetic(ctx, &commonv1.SyntheticTapePlan{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("PublishSynthetic(portfolio peer)=%v, want PermissionDenied", err)
	}
	strategyFactory := nativeFactory(ctx, t, ca, rpcsecurity.ServiceStrategy, discovery)
	strategy, err := nativestrategy.Workers(ctx, db, lease.OwnerID, cfg, strategyFactory, logger)
	if err != nil {
		t.Fatal(err)
	}
	var selected []deer.Worker
	for _, worker := range strategy {
		if strings.HasPrefix(worker.Name, native.SyntheticWorker) {
			selected = append(selected, worker)
		}
	}
	if len(selected) != 1 {
		t.Fatalf("synthetic workers=%d, want 1", len(selected))
	}
	runNativeWorkers(ctx, t, append(execution.Workers(), selected...))
	if mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		settings, err := native.LoadRuntime(ctx, db, cfg)
		if err != nil {
			t.Fatal(err)
		}
		assertSyntheticTradeOutage(ctx, t, syntheticTradeOutageCheck{
			market:  market,
			sdk:     sdk,
			db:      db,
			factory: strategyFactory,
			cfg:     settings.Himalaya,
			owner:   lease.OwnerID,
		})
		market.tradeUnavailable.Store(false)
	}
	nativeWait(ctx, t, func() bool {
		var rows []*modelv1.SyntheticTapeIntent
		if db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Find(&rows).Error != nil || len(rows) != 2 {
			return false
		}
		return sdk.syntheticCount() == 2 && rows[0].Result.GetState() == native.SyntheticPublished && rows[1].Result.GetState() == native.SyntheticPublished
	})
	if mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		assertSyntheticTradeReceipts(ctx, t, db)
	}
	var before modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", native.SyntheticStrategyKey(cfg.Himalaya.SyntheticTape.RunId, request.Symbol)).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	// A new worker starts its local sequence at zero, but the persisted slots
	// return the earlier results rather than issuing two more SDK publications.
	restarted, err := nativestrategy.Workers(ctx, db, lease.OwnerID, cfg, strategyFactory, logger)
	if err != nil {
		t.Fatal(err)
	}
	selected = nil
	for _, worker := range restarted {
		if strings.HasPrefix(worker.Name, native.SyntheticWorker) {
			selected = append(selected, worker)
		}
	}
	runNativeWorkers(ctx, t, selected)
	nativeWait(ctx, t, func() bool {
		var after modelv1.ExecutionShardLease
		return db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("id = ?", before.Id).First(&after).Error == nil && after.UpdateAt >= before.UpdateAt+100
	})
	sdk.mu.Lock()
	defer sdk.mu.Unlock()
	if len(sdk.synthetic) != 2 || len(sdk.orders) != 0 || sdk.next != 0 {
		t.Fatalf("SDK effects synthetic=%d orders=%d next=%d, want 2/0/0", len(sdk.synthetic), len(sdk.orders), sdk.next)
	}
	assertSyntheticSDKDirections(t, mode, sdk.synthetic)
}
