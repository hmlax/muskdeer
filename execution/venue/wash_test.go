package venue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hmlax/muskdeer/deer/logging"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type washTestClient struct {
	gateway.Client
	calls   int
	failure bool
}

func (c *washTestClient) WashTrade(_ context.Context, request *gateway.SyntheticRequest) (*gateway.SyntheticResult, error) {
	c.calls++
	if c.failure {
		return nil, errors.New("lost reply")
	}
	return &gateway.SyntheticResult{
		TradeID:     "wash-test",
		Price:       request.Price,
		Quantity:    request.Quantity,
		TimestampMS: uint64(time.Now().UnixMilli()),
	}, nil
}
func washTestContext(ctx context.Context) context.Context {
	return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains:   [][]*x509.Certificate{{{}}},
		PeerCertificates: []*x509.Certificate{{DNSNames: []string{rpcsecurity.ServiceStrategy}}},
	}}})
}
func TestWashEventNeedsNoDatabaseAndNeverReplaysUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger, err := logging.New(t.Context(), nil, logging.Identity{
			Service:  "execution",
			Instance: "wash-test",
		}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := logger.Close(); err != nil {
				t.Error(err)
			}
		})
		client := &washTestClient{}
		runtime := &Runtime{
			cfg: &venueconfig.RuntimeConfig{
				InstanceID:       7,
				Revision:         3,
				Template:         domain.StrategyTemplateWashTrade,
				OperationTimeout: time.Second,
				Venue: &common.VenuePolicy{
					Profiles: []*common.VenueProfilePolicy{{
						Symbol:       "BTCUSDT",
						PriceTick:    "0.1",
						QuantityStep: "0.001",
					}},
					SyntheticTape: &config.SyntheticTape{
						Enabled:         true,
						DevelopmentOnly: true,
						Mode:            config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK,
						Interval:        durationpb.New(100 * time.Millisecond),
						Profiles: []*config.SyntheticTapeProfile{{
							Symbol:   "BTCUSDT",
							Quantity: "0.01",
						}},
					},
				},
			},
			client:  &atomic.Pointer[gatewaySession]{},
			closing: &atomic.Bool{},
			logger:  logger,
			// db, store and lease repositories deliberately remain nil.
		}
		runtime.client.Store(&gatewaySession{Client: client})
		runtime.washControl.Store(&washControl{
			active:   true,
			observed: time.Now(),
		})
		input := &execution.WashTradeRequest{
			StrategyInstanceId: 7,
			StrategyRevision:   3,
			Symbol:             "BTCUSDT",
			Side:               string(domain.SideBuy),
			Price:              "101",
			Quantity:           "0.01",
			Source: &execution.WashTradeRequest_Book{Book: &common.VenueBook{
				Symbol:       "BTCUSDT",
				Source:       native.Source,
				Segment:      native.Segment,
				Sequence:     42,
				ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
				Publisher:    &common.VenueAuthority{AccountId: native.MarketKey("BTCUSDT")},
				Bids:         []*common.VenueLevel{{Price: "99"}},
				Asks:         []*common.VenueLevel{{Price: "101"}},
			}},
		}
		ctx := washTestContext(t.Context())
		for name, change := range map[string]func(*execution.WashTradeRequest){
			"revision": func(v *execution.WashTradeRequest) { v.StrategyRevision++ },
			"quantity": func(v *execution.WashTradeRequest) { v.Quantity = "1" },
			"stale": func(v *execution.WashTradeRequest) {
				v.GetBook().ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			},
		} {
			candidate := proto.CloneOf(input)
			change(candidate)
			if _, err := runtime.WashTrade(ctx, candidate); err == nil {
				t.Fatalf("invalid %s publication accepted", name)
			}
		}
		if _, err := runtime.WashTrade(t.Context(), input); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("unauthenticated = %v", err)
		}
		if client.calls != 0 {
			t.Fatal("rejected request reached SDK")
		}
		reply, err := runtime.WashTrade(ctx, input)
		if err != nil || reply.GetState() != native.SyntheticPublished || client.calls != 1 {
			t.Fatalf("direct publication = %v, %v, calls %d", reply, err, client.calls)
		}
		if _, err := runtime.WashTrade(ctx, input); status.Code(err) != codes.ResourceExhausted || client.calls != 1 {
			t.Fatalf("interval bypassed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		client.failure = true
		reply, err = runtime.WashTrade(ctx, input)
		if err != nil || reply.GetState() != native.SyntheticUnknown || client.calls != 2 {
			t.Fatalf("unknown receipt = %v, %v, calls %d", reply, err, client.calls)
		}
		runtime.washControl.Store(&washControl{observed: time.Now()})
		time.Sleep(100 * time.Millisecond)
		if _, err := runtime.WashTrade(ctx, input); err == nil || client.calls != 2 {
			t.Fatal("stopped instance sent a request")
		}
		runtime.washControl.Store(&washControl{
			active:   true,
			observed: time.Now().Add(-washControlAge - time.Second),
		})
		if _, err := runtime.WashTrade(ctx, input); err == nil || client.calls != 2 {
			t.Fatal("stale controls accepted")
		}
	})
}
