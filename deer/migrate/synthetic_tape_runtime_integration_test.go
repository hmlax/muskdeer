//go:build integration

package migrate

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/nacos"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	rpc "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/testcert"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
	nativestrategy "github.com/hmlax/muskdeer/strategy/native"
)

func TestSyntheticTapeWorkersUseSDKWithoutOrdersAndDoNotResetOnRestart(t *testing.T) {
	db, _, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	t.Cleanup(cancel)
	authority := liveAuthorityFixture(ctx, t, db, lease, request.Symbol)
	sdk := &nativeSDKFixture{orders: make(map[uint64]*pb.OrderRow)}
	address := startNativeSDK(t, sdk)
	ca := testcert.New(t)
	discovery := &nativeDiscoveryFixture{entries: make(map[string]nacos.Instance)}
	market := &nativeMarketFixture{authority: authority.Book.Publisher}
	discovery.entries[rpcsecurity.ServiceMarket] = nacos.Instance{
		ID:      "market-test",
		Service: rpcsecurity.ServiceMarket,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceMarket, func(server *grpc.Server) { rpc.RegisterMarketServer(server, market) }),
	}
	logger, err := logging.New(ctx, nil, logging.Identity{
		Service:  "execution",
		Instance: "synthetic-test",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
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
			Profiles: []*configv1.HimalayaProfile{{
				UserId:           request.UserID,
				FundId:           request.FundID,
				Symbol:           request.Symbol,
				Leverage:         10,
				MarketDataSource: native.Source,
				PriceTick:        "0.1",
				QuantityStep:     "0.001",
			}},
			SyntheticTape: &configv1.SyntheticTape{
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
	execution, err := himalaya.NewRuntime(db, lease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceExecution, discovery), logger)
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
	strategy, err := nativestrategy.Workers(db, lease.OwnerID, cfg, strategyFactory, logger)
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
	nativeWait(ctx, t, func() bool {
		var rows []*modelv1.SyntheticTapeIntent
		if db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Find(&rows).Error != nil || len(rows) != 2 {
			return false
		}
		return sdk.syntheticCount() == 2 && rows[0].Result.GetState() == native.SyntheticPublished && rows[1].Result.GetState() == native.SyntheticPublished
	})
	var before modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", native.SyntheticStrategyKey(cfg.Himalaya.SyntheticTape.RunId, request.Symbol)).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	// A new worker starts its local sequence at zero, but the persisted slots
	// return the earlier results rather than issuing two more SDK publications.
	restarted, err := nativestrategy.Workers(db, lease.OwnerID, cfg, strategyFactory, logger)
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
	if sdk.synthetic[0].Side != int32(pb.Side_SIDE_BUY) || sdk.synthetic[1].Side != int32(pb.Side_SIDE_SELL) {
		t.Fatal("synthetic directions did not alternate")
	}
}
