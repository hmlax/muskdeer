package native

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestMakerSnapshotIsFreshScopedAndOnlyInMemory(t *testing.T) {
	t.Parallel()
	profile := &common.VenueProfilePolicy{
		UserId: 1,
		FundId: 1,
		Symbol: "BTCUSDT",
	}
	runtime := &Runtime{
		memoryOnly: true,
		cfg:        &venueconfig.RuntimeConfig{Venue: &common.VenuePolicy{Profiles: []*common.VenueProfilePolicy{profile}}},
	}
	scope := values.Scope(profile)
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains:   [][]*x509.Certificate{{{}}},
		PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
	}}})
	if _, err := runtime.GetSnapshot(ctx, scope); status.Code(err) != codes.Unavailable {
		t.Fatalf("empty memory cache = %v", err)
	}
	snapshot := &common.VenueSnapshot{
		Scope:        proto.CloneOf(scope),
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
		Orders: []*common.VenueOrder{{
			OrderId: "1",
			Price:   "99",
		}},
	}
	if err := runtime.saveSnapshot(ctx, profile, domain.LeaseToken{}, snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Orders[0].Price = "1"
	got, err := runtime.GetSnapshot(ctx, scope)
	if err != nil || got.GetOrders()[0].Price != "99" {
		t.Fatalf("memory observation = %v, %v", got, err)
	}
	got.Orders[0].Price = "2"
	got, err = runtime.GetSnapshot(ctx, scope)
	if err != nil || got.GetOrders()[0].Price != "99" {
		t.Fatal("caller mutated shared snapshot")
	}
	if _, err := runtime.GetSnapshot(context.Background(), scope); status.Code(err) != codes.PermissionDenied {
		t.Fatal("snapshot bypassed mTLS")
	}
	foreign := proto.CloneOf(scope)
	foreign.FundId = 2
	if _, err := runtime.GetSnapshot(ctx, foreign); status.Code(err) != codes.InvalidArgument {
		t.Fatal("foreign scope read memory snapshot")
	}
	expired := proto.CloneOf(got)
	expired.ValidUntilMs = time.Now().UnixMilli() - 1
	runtime.latest.Store(expired)
	if _, err := runtime.GetSnapshot(ctx, scope); status.Code(err) != codes.Unavailable {
		t.Fatal("expired snapshot served from memory")
	}
}
