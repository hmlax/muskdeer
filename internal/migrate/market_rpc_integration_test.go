//go:build integration

package migrate

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/marketgrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketview"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestMarketRPCUsesVerifiedIdentityExactSourceAndCurrentFencedSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	feed, token, snapshots := marketSnapshotFixture(ctx, t, db)
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	server, err := marketgrpc.New(marketgrpc.Dependencies{
		Feeds:     feeds,
		Snapshots: snapshots,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, denied := marketRPCFixture(ctx, t, server)
	request := &marketv1.GetReferenceRequest{
		Source:       string(feed.Source),
		InstrumentId: feed.InstrumentID,
		Mode:         string(feed.Mode),
	}
	spoofed := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-service-id", rpcsecurity.ServiceStrategy))
	if _, err := readMarketReference(spoofed, denied, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("verified market identity forged strategy permissions: %v", err)
	}
	request.Source = string(domain.MarketSourceOKX)
	if _, err := readMarketReference(ctx, reader, request); status.Code(err) != codes.NotFound {
		t.Fatalf("missing OKX substituted Binance: %v", err)
	}
	request.Source = string(feed.Source)
	reading := marketReadingFixture(feed, 1, "100")
	reading.Bid, reading.Ask, reading.Mid = decimal.RequireFromString("99.999999999999999999"), decimal.NewFromInt(100), decimal.RequireFromString("99.9999999999999999995")
	if _, err := snapshots.Publish(ctx, token, 0, reading); err != nil {
		t.Fatal(err)
	}
	reply, err := readMarketReference(ctx, reader, request)
	if err != nil || reply.Reference.MidPrice != "99.9999999999999999995" || reply.Reference.Source != string(feed.Source) || reply.Reference.PublicationRevision != 1 {
		t.Fatalf("real typed mTLS reference: %v, %v", reply, err)
	}
	decoded, err := marketview.FromProto(reply.Reference)
	if err != nil || decoded.Reading.Selected(feed.Source, feed.InstrumentID, feed.Symbol, feed.Mode, time.Now(), time.Second) != nil {
		t.Fatalf("consumer observation did not retain exact provenance: %+v, %v", decoded, err)
	}
	assertMarketRPCIsolation(ctx, t, db, feeds, snapshots, reader, feed, request)
}

func assertMarketRPCIsolation(ctx context.Context, t *testing.T, db *gorm.DB, feeds domain.MarketFeedRepository, snapshots domain.MarketRepository, reader marketv1.MarketDataClient, feed domain.MarketFeedDefinition, request *marketv1.GetReferenceRequest) {
	t.Helper()
	other := feed
	other.ID, other.Source, other.Symbol = 0, domain.MarketSourceOKX, "BTC-USDT"
	okx, err := feeds.Create(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "market-okx")
	if err != nil {
		t.Fatal(err)
	}
	okxToken, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: okx.ID,
	}, owner, domain.MaxLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshots.Publish(ctx, okxToken, 0, marketReadingFixture(okx, 1, "200")); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).Update("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	reply, err := readMarketReference(ctx, reader, request)
	if err != nil || reply.Reference.Quality != string(domain.MarketQualityUnavailable) || reply.Reference.MidPrice != "0" {
		t.Fatalf("RPC returned executable expired source: %v, %v", reply, err)
	}
	request.Source = string(domain.MarketSourceOKX)
	reply, err = readMarketReference(ctx, reader, request)
	if err != nil || reply.Reference.Quality != string(domain.MarketQualityHealthy) || reply.Reference.MidPrice != "200" || reply.Reference.FeedId != okx.ID {
		t.Fatalf("one provider expiry poisoned healthy other source: %v, %v", reply, err)
	}
	request.Mode = string(domain.MarketModeLive)
	if _, err := readMarketReference(ctx, reader, request); status.Code(err) != codes.NotFound {
		t.Fatalf("live selection consumed a replay source: %v", err)
	}
}

func readMarketReference(ctx context.Context, client marketv1.MarketDataClient, request *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error) {
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return client.GetReference(call, request)
}

func marketRPCFixture(ctx context.Context, t *testing.T, handler *marketgrpc.Server) (marketv1.MarketDataClient, marketv1.MarketDataClient) {
	t.Helper()
	ca := testcert.New(t)
	cfg := &configv1.ServiceConfig{
		Name:            "market",
		InstanceId:      "market-rpc-test",
		Version:         "test",
		StartupTimeout:  durationpb.New(3 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		GrpcSecurity: &configv1.GRPCServerSecurity{
			Mtls:                ca.Issue(t, rpcsecurity.ServiceMarket, false),
			AllowedPeerServices: []string{rpcsecurity.ServiceStrategy, rpcsecurity.ServiceAPI, rpcsecurity.ServiceMarket},
		},
	}
	server, cleanup, err := deer.NewDeer(ctx, cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
			marketv1.RegisterMarketDataServer(registrar, handler)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	finished := make(chan error, 1)
	go func() { finished <- server.Run(run) }()
	t.Cleanup(func() {
		cancel()
		stop, cancelStop := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
		defer cancelStop()
		if err := cleanup(stop); err != nil {
			t.Error(err)
		}
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	return marketRPCClient(t, ca, server.GRPCAddress(), rpcsecurity.ServiceStrategy), marketRPCClient(t, ca, server.GRPCAddress(), rpcsecurity.ServiceMarket)
}

func marketRPCClient(t *testing.T, ca *testcert.Authority, address, service string) marketv1.MarketDataClient {
	t.Helper()
	identity, err := rpcsecurity.Client(ca.Issue(t, service, false), service, rpcsecurity.ServiceMarket)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(credentials.NewTLS(identity)), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	return marketv1.NewMarketDataClient(connection)
}
