package usdm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type bookTestStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan *common.VenueBook
}

func (s *bookTestStream) Context() context.Context       { return s.ctx }
func (s *bookTestStream) Send(v *common.VenueBook) error { s.sent <- v; return nil }
func TestBookSubscriptionOwnsNoDatabaseAndCleansUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stream := &bookTestStream{ctx: ctx, sent: make(chan *common.VenueBook, 2)}
		runtime := &Runtime{books: map[string]*common.VenueBook{"BTCUSDT": {
			Symbol: "BTCUSDT", Sequence: 1, ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
			Bids: []*common.VenueLevel{{Price: "99"}},
		}}}
		request := &market.BookRequest{Symbol: "BTCUSDT"}
		if err := runtime.SubscribeBooks(request, stream); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("unauthenticated = %v", err)
		}
		stream.ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{{}}}, PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
		}}})
		done := make(chan error, 1)
		go func() { done <- runtime.SubscribeBooks(request, stream) }()
		synctest.Wait()
		first := <-stream.sent
		first.Bids[0].Price = "mutated"
		runtime.mu.Lock()
		if runtime.books["BTCUSDT"].Bids[0].Price != "99" {
			t.Fatal("subscriber mutated cached book")
		}
		for sequence := int64(2); sequence <= 100; sequence++ {
			runtime.bookReaders.publish(&common.VenueBook{Symbol: "BTCUSDT", Sequence: sequence, ValidUntilMs: time.Now().Add(time.Second).UnixMilli()})
		}
		runtime.mu.Unlock()
		synctest.Wait()
		var last int64
		for len(stream.sent) > 0 {
			last = (<-stream.sent).Sequence
		}
		if last != 100 {
			t.Fatalf("coalesced source = %d, want 100", last)
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("stream close = %v", err)
		}
		runtime.mu.RLock()
		defer runtime.mu.RUnlock()
		if len(runtime.bookReaders) != 0 {
			t.Fatal("subscriber leaked")
		}
	})
}
