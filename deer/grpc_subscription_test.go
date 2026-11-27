package deer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
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

func TestOnlyBoundedAuthenticatedQuoteDiagnosticsMayOutliveUnaryDeadline(t *testing.T) {
	for _, kind := range []string{"allowed", "no_deadline", "too_long", "unauthenticated", "execution_identity", "other_method", "not_server_stream"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			service := rpcsecurity.ServiceStrategy
			method := execution.Execution_SubscribeQuoteFlows_FullMethodName
			serverStream := true
			switch kind {
			case "no_deadline":
				ctx = t.Context()
			case "too_long":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(t.Context(), time.Hour)
				defer stop()
			case "execution_identity":
				service = rpcsecurity.ServiceExecution
			case "other_method":
				method = "/grpc.health.v1.Health/Watch"
			case "not_server_stream":
				serverStream = false
			}
			if kind != "unauthenticated" {
				ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
					VerifiedChains:   [][]*x509.Certificate{{{}}},
					PeerCertificates: []*x509.Certificate{{DNSNames: []string{service}}},
				}}})
			}
			called := false
			err := streamDeadlineInterceptor(time.Second)(nil, deadlineTestStream{ctx: ctx}, &grpc.StreamServerInfo{
				FullMethod:     method,
				IsServerStream: serverStream,
			}, func(any, grpc.ServerStream) error { called = true; return nil })
			if kind == "allowed" {
				if err != nil || !called {
					t.Fatalf("bounded authenticated diagnostic rejected: %v", err)
				}
			} else if status.Code(err) != codes.InvalidArgument || called {
				t.Fatalf("diagnostic deadline exception widened for %s: %v", kind, err)
			}
		})
	}
}

func (s deadlineTestStream) Context() context.Context { return s.ctx }
func TestOnlyAuthenticatedMarketSubscriptionsMayOutliveUnaryDeadline(t *testing.T) {
	authenticated := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains:   [][]*x509.Certificate{{{}}},
		PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
	}}})
	interceptor := streamDeadlineInterceptor(time.Second)
	for _, method := range []string{market.Market_SubscribeBooks_FullMethodName, market.Market_SubscribeTrades_FullMethodName} {
		called := false
		handler := func(any, grpc.ServerStream) error { called = true; return nil }
		if err := interceptor(nil, deadlineTestStream{ctx: authenticated}, &grpc.StreamServerInfo{
			FullMethod:     method,
			IsServerStream: true,
		}, handler); err != nil || !called {
			t.Fatalf("authenticated subscription rejected: %v", err)
		}
		called = false
		if err := interceptor(nil, deadlineTestStream{ctx: t.Context()}, &grpc.StreamServerInfo{
			FullMethod:     method,
			IsServerStream: true,
		}, handler); status.Code(err) != codes.InvalidArgument || called {
			t.Fatalf("unauthenticated exception: %v", err)
		}
	}
	if err := interceptor(nil, deadlineTestStream{ctx: authenticated}, &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Watch"}, func(any, grpc.ServerStream) error { return nil }); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("other stream lost deadline: %v", err)
	}
}
