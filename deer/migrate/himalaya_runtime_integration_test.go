//go:build integration

package migrate

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"gorm.io/gorm"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	markethimalayav1 "github.com/hmlax/muskdeer/deer/pb/market/himalaya/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/testcert"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
	nativeportfolio "github.com/hmlax/muskdeer/portfolio/native"
	nativestrategy "github.com/hmlax/muskdeer/strategy/native"
)

func TestHimalayaNativeWorkersQuoteAndCancelWhenSourceOwnershipIsLost(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{})
}

func TestHimalayaNativeWorkersRecoverHistoryAcrossBoundedCalls(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{legacy: 40})
}

func TestHimalayaNativeRepricingKeepsBothSidesWhileCancelProofIsPending(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{reprice: 1})
}

func TestHimalayaNativePriceJumpSeedsSafeSideBeforeCancellingOldQuote(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{reprice: 5})
}

func TestHimalayaNativeReplyLossRecoversPositiveHistoryWithoutResubmission(t *testing.T) {
	testNativeWorkers(t, nativeWorkerScenario{lostReply: true})
}

type nativeWorkerScenario struct {
	legacy    int
	reprice   int64
	lostReply bool
	depth     uint32
	resize    bool
}

func testNativeWorkers(t *testing.T, scenario nativeWorkerScenario) {
	t.Helper()
	legacy, reprice, lostReply := scenario.legacy, scenario.reprice, scenario.lostReply
	depth := max(1, scenario.depth)
	db, store, executionLease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	if depth > 1 {
		maximumNotional := "2000"
		// This scenario needs forty old/new quote reservations in addition
		// to the fixture's retained 900 obligation. Existing tests keep 1000.
		if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", executionLease.Key.AccountID).
			Update("maximum_notional", maximumNotional).Error; err != nil {
			t.Fatal(err)
		}
	}
	authority := liveAuthorityFixture(ctx, t, db, executionLease, request.Symbol)
	configureNativeProfileFixture(ctx, t, db, request)
	sdk := &nativeSDKFixture{orders: make(map[uint64]*pb.OrderRow)}
	sdk.lostTerminalReply.Store(lostReply)
	if reprice > 0 || scenario.resize {
		sdk.historyEntered = make(chan struct{}, 1)
		sdk.historyRelease = make(chan struct{})
	}
	if legacy > 0 {
		sdk.historyDelay = 40 * time.Millisecond
		for i := range legacy {
			id := uint64(i + 100)
			sdk.orders[id] = &pb.OrderRow{
				OrderId:  id,
				UserId:   request.UserID,
				FundId:   request.FundID,
				Symbol:   request.Symbol,
				ClientId: native.LegacyPrefix + request.Symbol + "_" + strconv.Itoa(i),
				Price:    "99.9",
				Quantity: 100,
				Side:     pb.Side_SIDE_BUY,
				Status:   native.StatusCanceled,
			}
			if _, err := store.PrepareCancel(ctx, executionLease, himalaya.CancelRequest{
				Scope: himalaya.Scope{
					UserID: request.UserID,
					FundID: request.FundID,
					Symbol: request.Symbol,
				},
				OrderID: id,
			}); err != nil {
				t.Fatal(err)
			}
			sdk.next = id
		}
	}
	sdkAddress := startNativeSDK(t, sdk)
	ca := testcert.New(t)
	discovery := &nativeDiscoveryFixture{entries: make(map[string]nacos.Instance)}
	sourceLeases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	market := &nativeMarketFixture{
		authority: authority.Book.Publisher,
		depth:     depth,
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
	discovery.entries[rpcsecurity.ServiceMarket] = nacos.Instance{
		ID:      "market-test",
		Service: rpcsecurity.ServiceMarket,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceMarket, func(server *grpc.Server) { markethimalayav1.RegisterMarketServer(server, market) }),
	}
	logger := nativeWorkerLogger(ctx, t, executionLease.OwnerID)
	t.Setenv("NATIVE_TEST_TOKEN", "test-token")
	cfg := &configv1.BusinessRuntime{
		Enabled:          true,
		ExecutionVenue:   native.Venue,
		PollInterval:     durationpb.New(100 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Himalaya: &configv1.HimalayaRuntime{
			Target:           sdkAddress,
			TokenEnv:         "NATIVE_TEST_TOKEN",
			Depth:            depth,
			QuantityFraction: "0.1",
			LeaseTtl:         durationpb.New(10 * time.Second),
		},
	}
	if depth > 1 {
		cfg.OperationTimeout = durationpb.New(2 * time.Second)
	}
	execution, err := himalaya.NewRuntime(ctx, db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceExecution, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	discovery.entries[rpcsecurity.ServiceExecution] = nacos.Instance{
		ID:      "execution-sdk-test",
		Service: rpcsecurity.ServiceExecution,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceExecution, func(server *grpc.Server) { execution.RegisterGRPC(server) }),
	}
	portfolio, err := nativeportfolio.NewRuntime(ctx, db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServicePortfolio, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	discovery.entries[rpcsecurity.ServicePortfolio] = nacos.Instance{
		ID:      "portfolio-test",
		Service: rpcsecurity.ServicePortfolio,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServicePortfolio, func(server *grpc.Server) { portfolio.RegisterGRPC(server) }),
	}
	strategy, err := nativestrategy.Workers(ctx, db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceStrategy, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	workers := append(execution.Workers(), portfolio.Workers()...)
	workers = append(workers, strategy...)
	runNativeWorkers(ctx, t, workers)
	nativeWait(ctx, t, func() bool {
		pending, err := store.PendingIDs(ctx, executionLease.Key.AccountID)
		return err == nil && len(pending) == int(2*depth) && sdk.active() == int(2*depth)
	})
	if pending, err := store.PendingIDs(ctx, executionLease.Key.AccountID); err != nil || len(pending) != int(2*depth) {
		t.Fatalf("PendingIDs(after history recovery) = %d, %v, want %d live orders", len(pending), err, 2*depth)
	}
	if lostReply {
		assertLostReplyRecovery(t, db, sdk)
	}
	if scenario.resize {
		t.Log("initial twenty-level ladder ready")
		assertNativeRollingResize(ctx, t, nativeRollingCheck{
			sdk:    sdk,
			market: market,
			depth:  int(depth),
		})
	}
	if reprice > 0 {
		assertNativeReprice(ctx, t, nativeRepriceCheck{
			sdk:    sdk,
			market: market,
			offset: reprice,
		})
	}
	var count int64
	if err := db.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Where("account_id = ?", native.ScopeKey(10001, 10001)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("native portfolio projection missing: count=%d err=%v", count, err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", authority.Book.Publisher.AccountId).UpdateColumn("epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	market.stop.Store(true)
	if scenario.resize {
		t.Log("source ownership lost; awaiting controlled cancellation")
	}
	nativeWait(ctx, t, func() bool { return sdk.active() == 0 })
}

func assertLostReplyRecovery(t *testing.T, db *gorm.DB, sdk *nativeSDKFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var recovered []*executionmodelv1.VenueOrderIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Where("state = ? AND order_id != ''", himalaya.IntentTerminal).Find(&recovered).Error; err != nil || len(recovered) != 1 || recovered[0].OrderId != "1" {
		t.Fatalf("lost reply native fact not recovered: %v, %v", recovered, err)
	}
	sdk.mu.Lock()
	calls := len(sdk.orders)
	sdk.mu.Unlock()
	if calls != 3 {
		t.Fatalf("lost reply produced %d native orders, want one terminal original plus two live quotes", calls)
	}
}

func nativeWait(ctx context.Context, t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("native worker observation did not converge")
		}
		if err := native.Wait(ctx, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
}

func runNativeWorkers(ctx context.Context, t *testing.T, workers []deer.Worker) {
	t.Helper()
	work, cancel := context.WithCancel(ctx)
	var running sync.WaitGroup
	results := make(chan error, len(workers))
	for _, worker := range workers {
		running.Go(func() { results <- worker.Run(work) })
	}
	t.Cleanup(func() {
		cancel()
		running.Wait()
		close(results)
		for err := range results {
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}
	})
}

type nativeDiscoveryFixture struct{ entries map[string]nacos.Instance }

func (d *nativeDiscoveryFixture) GetService(_ context.Context, service string) ([]nacos.Instance, error) {
	return []nacos.Instance{d.entries[service]}, nil
}

func nativeFactory(ctx context.Context, t *testing.T, ca *testcert.Authority, service string, discovery *nativeDiscoveryFixture) *rpcclient.Factory {
	t.Helper()
	cfg := businessRPCConfig(ca.Issue(t, service, false))
	cfg.AllowedServices = []string{rpcsecurity.ServiceMarket, rpcsecurity.ServiceExecution, rpcsecurity.ServicePortfolio}
	factory, err := rpcclient.New(ctx, cfg, service, discovery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	return factory
}

func nativeTLSServer(t *testing.T, ca *testcert.Authority, service string, register func(*grpc.Server)) string {
	t.Helper()
	security, err := rpcsecurity.Server(&configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, service, false),
		AllowedPeerServices: []string{rpcsecurity.ServiceMarket, rpcsecurity.ServiceExecution, rpcsecurity.ServicePortfolio, rpcsecurity.ServiceStrategy},
	}, service)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)), grpc.WaitForHandlers(true))
	register(server)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	return listener.Addr().String()
}
