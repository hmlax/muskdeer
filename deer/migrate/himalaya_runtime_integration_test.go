//go:build integration

package migrate

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/testcert"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
	nativeportfolio "github.com/hmlax/muskdeer/portfolio/native"
	nativestrategy "github.com/hmlax/muskdeer/strategy/native"
)

func TestHimalayaNativeWorkersQuoteAndCancelWhenSourceOwnershipIsLost(t *testing.T) {
	testNativeWorkers(t, 0, 0)
}

func TestHimalayaNativeWorkersRecoverHistoryAcrossBoundedCalls(t *testing.T) {
	testNativeWorkers(t, 40, 0)
}

func TestHimalayaNativeRepricingKeepsBothSidesWhileCancelProofIsPending(t *testing.T) {
	testNativeWorkers(t, 0, 1)
}

func TestHimalayaNativePriceJumpSeedsSafeSideBeforeCancellingOldQuote(t *testing.T) {
	testNativeWorkers(t, 0, 5)
}

func testNativeWorkers(t *testing.T, legacy int, reprice int64) {
	t.Helper()
	db, store, executionLease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	authority := liveAuthorityFixture(ctx, t, db, executionLease, request.Symbol)
	sdk := &nativeSDKFixture{orders: make(map[uint64]*pb.OrderRow)}
	if reprice > 0 {
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
	market := &nativeMarketFixture{authority: authority.Book.Publisher}
	discovery.entries[rpcsecurity.ServiceMarket] = nacos.Instance{
		ID:      "market-test",
		Service: rpcsecurity.ServiceMarket,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceMarket, func(server *grpc.Server) { himalayav1.RegisterMarketServer(server, market) }),
	}
	logger, err := logging.New(ctx, nil, logging.Identity{
		Service:  "execution",
		Instance: "sdk-test",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("NATIVE_TEST_TOKEN", "test-token")
	cfg := &configv1.BusinessRuntime{
		Enabled:          true,
		ExecutionVenue:   native.Venue,
		PollInterval:     durationpb.New(100 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Himalaya: &configv1.HimalayaRuntime{
			Target:           sdkAddress,
			TokenEnv:         "NATIVE_TEST_TOKEN",
			Depth:            1,
			QuantityFraction: "0.1",
			LeaseTtl:         durationpb.New(10 * time.Second),
			Profiles: []*configv1.HimalayaProfile{{
				UserId:               10001,
				FundId:               10001,
				Symbol:               "BTCUSDT",
				Leverage:             10,
				MarketDataSource:     native.Source,
				PriceTick:            "0.1",
				QuantityStep:         "0.001",
				MaximumNotional:      "1000",
				MaximumOrderNotional: "1000",
			}},
		},
	}
	execution, err := himalaya.NewRuntime(db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceExecution, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	discovery.entries[rpcsecurity.ServiceExecution] = nacos.Instance{
		ID:      "execution-sdk-test",
		Service: rpcsecurity.ServiceExecution,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServiceExecution, func(server *grpc.Server) { execution.RegisterGRPC(server) }),
	}
	portfolio, err := nativeportfolio.NewRuntime(db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServicePortfolio, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	discovery.entries[rpcsecurity.ServicePortfolio] = nacos.Instance{
		ID:      "portfolio-test",
		Service: rpcsecurity.ServicePortfolio,
		Address: nativeTLSServer(t, ca, rpcsecurity.ServicePortfolio, func(server *grpc.Server) { portfolio.RegisterGRPC(server) }),
	}
	strategy, err := nativestrategy.Workers(db, executionLease.OwnerID, cfg, nativeFactory(ctx, t, ca, rpcsecurity.ServiceStrategy, discovery), logger)
	if err != nil {
		t.Fatal(err)
	}
	workers := append(execution.Workers(), portfolio.Workers()...)
	workers = append(workers, strategy...)
	runNativeWorkers(ctx, t, workers)
	nativeWait(ctx, t, func() bool {
		pending, err := store.PendingIDs(ctx, executionLease.Key.AccountID)
		return err == nil && len(pending) == 2 && sdk.active() == 2
	})
	if pending, err := store.PendingIDs(ctx, executionLease.Key.AccountID); err != nil || len(pending) != 2 {
		t.Fatalf("PendingIDs(after history recovery) = %d, %v, want two live orders", len(pending), err)
	}
	if reprice > 0 {
		release := sync.OnceFunc(func() { close(sdk.historyRelease) })
		t.Cleanup(release)
		sdk.historyHold.Store(true)
		market.offset.Store(reprice)
		select {
		case <-sdk.historyEntered:
		case <-ctx.Done():
			t.Fatal("repricing did not reach terminal-history barrier")
		}
		sdk.mu.Lock()
		buys, sells := 0, 0
		for _, order := range sdk.orders {
			if native.Active(order.Status) {
				if order.Side == pb.Side_SIDE_BUY {
					buys++
				} else {
					sells++
				}
			}
		}
		sdk.mu.Unlock()
		if buys == 0 || sells == 0 {
			t.Fatalf("repricing while cancel proof pending has buys=%d sells=%d, want both sides present", buys, sells)
		}
		release()
		nativeWait(ctx, t, func() bool {
			sdk.mu.Lock()
			defer sdk.mu.Unlock()
			current := 0
			bid := decimal.RequireFromString("99.9").Add(decimal.NewFromInt(reprice).Shift(-1)).String()
			ask := decimal.RequireFromString("100.1").Add(decimal.NewFromInt(reprice).Shift(-1)).String()
			for _, order := range sdk.orders {
				if native.Active(order.Status) && (order.Price == bid || order.Price == ask) {
					current++
				}
			}
			return current == 2
		})
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Where("account_id = ?", native.ScopeKey(10001, 10001)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("native portfolio projection missing: count=%d err=%v", count, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", authority.Book.Publisher.AccountId).UpdateColumn("epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	market.stop.Store(true)
	nativeWait(ctx, t, func() bool { return sdk.active() == 0 })
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
