//go:build integration

package migrate

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestIndependentMarketCommandsServeSelectedSourcesOverDiscoveryAndMTLS(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	instrument := marketInstrumentFixture(ctx, t, db)
	binance, err := feeds.Create(ctx, domain.MarketFeedDefinition{
		Source:        domain.MarketSourceBinance,
		InstrumentID:  instrument,
		Symbol:        "BTCUSDT",
		Mode:          domain.MarketModeReplay,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        marketquality.DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	okxInput := binance
	okxInput.ID, okxInput.Source, okxInput.Symbol = 0, domain.MarketSourceOKX, "BTC-USDT"
	okx, err := feeds.Create(ctx, okxInput)
	if err != nil {
		t.Fatal(err)
	}
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	ca := testcert.New(t)
	binanceConfig := marketProcessConfig(t, ca, postgres, naming, binance.ID, "binance", "99.999999999999999999", "100")
	binancePID := startBusinessProcess(ctx, t, binanceConfig, observer)
	okxConfig := marketProcessConfig(t, ca, postgres, naming, okx.ID, "okx", "199.99", "200.01")
	okxPID := startBusinessProcess(ctx, t, okxConfig, observer)
	if binancePID == okxPID || binancePID == os.Getpid() || okxPID == os.Getpid() {
		t.Fatal("market sources did not have independent command processes")
	}
	rpcConfig := businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceStrategy, false))
	rpcConfig.AllowedServices = []string{rpcsecurity.ServiceMarket}
	factory, err := rpcclient.New(ctx, rpcConfig, rpcsecurity.ServiceStrategy, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	reader, err := marketgrpc.NewReader(factory)
	if err != nil {
		t.Fatal(err)
	}
	first := waitProcessReference(ctx, t, reader, binance, domain.MarketQualityHealthy)
	second := waitProcessReference(ctx, t, reader, okx, domain.MarketQualityHealthy)
	if first.Reading.Mid.String() != "99.9999999999999999995" || second.Reading.Mid.String() != "200" || first.FeedID == second.FeedID || first.OwnerID == second.OwnerID {
		t.Fatalf("process source identity or precision changed: binance=%+v, okx=%+v", first, second)
	}
	process, err := os.FindProcess(binancePID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	closed := waitProcessReference(ctx, t, reader, binance, domain.MarketQualityUnavailable)
	stillHealthy := waitProcessReference(ctx, t, reader, okx, domain.MarketQualityHealthy)
	if !closed.Reading.Mid.IsZero() || stillHealthy.Reading.Mid.String() != "200" || stillHealthy.OwnerEpoch != second.OwnerEpoch {
		t.Fatal("one source shutdown switched or poisoned the other")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 0)
}

func marketProcessConfig(t *testing.T, ca *testcert.Authority, postgres *configv1.Postgres, naming *configv1.Nacos, feedID int64, instance, bid, ask string) *configv1.ServiceConfig {
	t.Helper()
	cfg := businessProcessConfig(t, ca, postgres, nil, naming, "market")
	cfg.InstanceId = "business-market-" + instance
	cfg.Business.OperationTimeout = durationpb.New(200 * time.Millisecond)
	cfg.Business.Market = &configv1.MarketRuntime{
		LeaseTtl: durationpb.New(3 * time.Second),
		Feeds: []*configv1.MarketFeedRuntime{{
			FeedId:         feedID,
			ReplayBid:      bid,
			ReplayAsk:      ask,
			ReplayQuantity: "1",
			ReplayInterval: durationpb.New(100 * time.Millisecond),
		}},
	}
	return cfg
}

func waitProcessReference(ctx context.Context, t *testing.T, reader domain.MarketReader, feed domain.MarketFeedDefinition, quality domain.MarketQuality) domain.MarketReference {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, time.Second)
		value, err := reader.Reference(call, feed.Source, feed.InstrumentID, feed.Mode)
		cancel()
		if err == nil && value.Reading.Quality == quality {
			return value
		}
		if err != nil && status.Code(err) != codes.NotFound && status.Code(err) != codes.Unavailable {
			t.Fatalf("selected process reference failed: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("selected process reference did not reach expected quality")
		case <-ticker.C:
		}
	}
}
