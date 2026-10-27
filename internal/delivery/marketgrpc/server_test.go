package marketgrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
)

type observationStore struct {
	feed  domain.MarketFeedDefinition
	value domain.MarketPublication
	finds int
	gets  int
}

func (s *observationStore) Find(_ context.Context, source domain.MarketSource, instrumentID int64, mode domain.MarketMode) (domain.MarketFeedDefinition, error) {
	s.finds++
	if s.feed.Source != source || s.feed.InstrumentID != instrumentID || s.feed.Mode != mode {
		return domain.MarketFeedDefinition{}, domain.ErrMarketFeedMissing
	}
	return s.feed, nil
}

func (s *observationStore) Get(_ context.Context, _ int64) (domain.MarketPublication, error) {
	s.gets++
	return s.value, nil
}

func TestReadRPCRequiresVerifiedAllowedServiceAndNativeDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := storeFixture()
	server, err := New(Dependencies{
		Feeds:     store,
		Snapshots: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := requestFixture()
	spoof := metadata.NewIncomingContext(ctx, metadata.Pairs("x-service-id", rpcsecurity.ServiceStrategy))
	if _, err := server.GetReference(spoof, request); status.Code(err) != codes.PermissionDenied || store.finds != 0 {
		t.Fatal("metadata impersonated a verified service")
	}
	if _, err := server.GetReference(verified(ctx, rpcsecurity.ServiceMarket), request); status.Code(err) != codes.PermissionDenied || store.finds != 0 {
		t.Fatal("method allowlist was not enforced")
	}
	for _, service := range []string{rpcsecurity.ServiceAPI, rpcsecurity.ServiceStrategy, rpcsecurity.ServiceExecution, rpcsecurity.ServicePortfolio, rpcsecurity.ServiceCron} {
		reply, err := server.GetReference(verified(ctx, service), request)
		if err != nil || reply.Reference.Source != string(domain.MarketSourceBinance) || reply.Reference.MidPrice != "100" || reply.Reference.PublicationRevision != 3 {
			t.Fatalf("typed market read: %v, %v", reply, err)
		}
	}
	unbounded := verified(context.WithoutCancel(ctx), rpcsecurity.ServiceStrategy)
	if _, err := server.GetReference(unbounded, request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded request reached storage")
	}
	before := store.finds
	for _, request := range []*marketv1.GetReferenceRequest{
		nil,
		{
			Source:       string(domain.MarketSourceBybit),
			InstrumentId: 1,
			Mode:         string(domain.MarketModeReplay),
		},
		{
			Source:       string(domain.MarketSourceBinance),
			InstrumentId: 0,
			Mode:         string(domain.MarketModeReplay),
		},
		{
			Source:       string(domain.MarketSourceBinance),
			InstrumentId: 1,
		},
	} {
		if _, err := server.GetReference(verified(ctx, rpcsecurity.ServiceStrategy), request); status.Code(err) != codes.InvalidArgument || store.finds != before {
			t.Fatal("invalid selection reached storage")
		}
	}
	request.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.GetReference(verified(ctx, rpcsecurity.ServiceStrategy), request); status.Code(err) != codes.InvalidArgument || store.finds != before {
		t.Fatal("unknown protocol fields were accepted")
	}
}

func TestReadRPCDoesNotSubstituteAnotherSourceOrMismatchedProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := storeFixture()
	server, err := New(Dependencies{
		Feeds:     store,
		Snapshots: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := requestFixture()
	request.Source = string(domain.MarketSourceOKX)
	if _, err := server.GetReference(verified(ctx, rpcsecurity.ServiceStrategy), request); status.Code(err) != codes.NotFound || store.gets != 0 {
		t.Fatal("missing source used an available alternative")
	}
	request.Source = string(domain.MarketSourceBinance)
	store.value.Reading.Mode = domain.MarketModeLive
	if _, err := server.GetReference(verified(ctx, rpcsecurity.ServiceStrategy), request); status.Code(err) != codes.Internal {
		t.Fatal("storage returned a different mode without rejection")
	}
}

func verified(ctx context.Context, service string) context.Context {
	certificate := &x509.Certificate{DNSNames: []string{service}}
	return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}}})
}

func requestFixture() *marketv1.GetReferenceRequest {
	return &marketv1.GetReferenceRequest{
		Source:       string(domain.MarketSourceBinance),
		InstrumentId: 1,
		Mode:         string(domain.MarketModeReplay),
	}
}

func storeFixture() *observationStore {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &observationStore{
		feed: domain.MarketFeedDefinition{
			ID:            1,
			Source:        domain.MarketSourceBinance,
			InstrumentID:  1,
			Symbol:        "BTCUSDT",
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Policy:        marketquality.DefaultPolicy(),
		},
		value: domain.MarketPublication{MarketReference: domain.MarketReference{
			FeedID:         1,
			OwnerID:        "market.01234567890123456789012345678901",
			OwnerEpoch:     1,
			AuthorityUntil: now.Add(5 * time.Second),
			Revision:       3,
			Reading: domain.MarketReading{
				Source:        domain.MarketSourceBinance,
				InstrumentID:  1,
				Symbol:        "BTCUSDT",
				Mode:          domain.MarketModeReplay,
				SourceVersion: domain.MarketSourceVersion,
				Connection:    1,
				Generation:    1,
				Sequence:      3,
				Quality:       domain.MarketQualityHealthy,
				Reason:        domain.MarketReasonNone,
				Bid:           decimal.RequireFromString("99.99"),
				BidQuantity:   decimal.NewFromInt(1),
				Ask:           decimal.RequireFromString("100.01"),
				AskQuantity:   decimal.NewFromInt(1),
				Mid:           decimal.NewFromInt(100),
				EventTime:     now.Add(-time.Millisecond),
				ReceivedAt:    now,
				EvaluatedAt:   now,
				ValidUntil:    now.Add(time.Second),
			},
		}},
	}
}
