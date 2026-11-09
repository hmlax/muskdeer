package deer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type deadlineTestStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s deadlineTestStream) Context() context.Context { return s.ctx }
func TestOnlyAuthenticatedMarketSubscriptionsMayOutliveUnaryDeadline(t *testing.T) {
	authenticated := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{{}}}, PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
	}}})
	interceptor := streamDeadlineInterceptor(time.Second)
	for _, method := range []string{market.Market_SubscribeBooks_FullMethodName, market.Market_SubscribeTrades_FullMethodName} {
		called := false
		handler := func(any, grpc.ServerStream) error { called = true; return nil }
		if err := interceptor(nil, deadlineTestStream{ctx: authenticated}, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, handler); err != nil || !called {
			t.Fatalf("authenticated subscription rejected: %v", err)
		}
		called = false
		if err := interceptor(nil, deadlineTestStream{ctx: t.Context()}, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, handler); status.Code(err) != codes.InvalidArgument || called {
			t.Fatalf("unauthenticated exception: %v", err)
		}
	}
	if err := interceptor(nil, deadlineTestStream{ctx: authenticated}, &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Watch"}, func(any, grpc.ServerStream) error { return nil }); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("other stream lost deadline: %v", err)
	}
}
