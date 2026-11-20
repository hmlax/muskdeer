package usdm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type bufferedTradeStream struct {
	grpc.ServerStream
	ctx     context.Context
	started chan struct{}
	release chan struct{}
	sent    chan *common.VenueTrade
}

func (s *bufferedTradeStream) Context() context.Context { return s.ctx }
func (s *bufferedTradeStream) Send(trade *common.VenueTrade) error {
	if trade.EventId == "40" {
		close(s.started)
		select {
		case <-s.release:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	s.sent <- trade
	return nil
}

func TestTradeSubscriptionBuffersBurstsAndReportsOverflow(t *testing.T) {
	for _, kind := range []string{"burst", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
					VerifiedChains:   [][]*x509.Certificate{{{}}},
					PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
				}}})
				stream := &bufferedTradeStream{
					ctx:     ctx,
					started: make(chan struct{}),
					release: make(chan struct{}),
					sent:    make(chan *common.VenueTrade, native.WashSourceQueueCapacity+1),
				}
				runtime := &Runtime{trades: make(map[string]*common.VenueTrade)}
				done := make(chan error, 1)
				go func() { done <- runtime.SubscribeTrades(&pb.TradeRequest{Symbol: "BTCUSDT"}, stream) }()
				synctest.Wait()
				publish := func(id int) {
					runtime.mu.Lock()
					defer runtime.mu.Unlock()
					runtime.tradeReaders.publish(&common.VenueTrade{
						Symbol:       "BTCUSDT",
						EventId:      strconv.Itoa(id),
						ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
					})
				}
				publish(40)
				synctest.Wait()
				<-stream.started
				count := 3
				if kind == "overflow" {
					count = native.WashSourceQueueCapacity + 1
				}
				for index := 1; index <= count; index++ {
					publish(40 + index)
				}
				close(stream.release)
				synctest.Wait()
				if kind == "overflow" {
					select {
					case err := <-done:
						if status.Code(err) != codes.ResourceExhausted {
							t.Fatalf("overflow = %v", err)
						}
					default:
						t.Fatal("trade overflow silently overwrote source observations")
					}
				} else {
					if len(stream.sent) != 4 {
						t.Fatalf("source burst was coalesced: %d observations", len(stream.sent))
					}
					for id := 40; id <= 43; id++ {
						if got := (<-stream.sent).EventId; got != strconv.Itoa(id) {
							t.Fatalf("source order changed: %s", got)
						}
					}
					cancel()
					synctest.Wait()
					if err := <-done; !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				}
				runtime.mu.RLock()
				defer runtime.mu.RUnlock()
				if len(runtime.tradeReaders) != 0 {
					t.Fatal("subscriber outlived its scope")
				}
			})
		})
	}
}
