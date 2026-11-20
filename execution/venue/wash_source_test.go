package venue

import (
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hmlax/muskdeer/deer/logging"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestSourceWashRejectsDuplicateUnknownWithoutInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger, err := logging.New(t.Context(), nil, logging.Identity{
			Service:  "execution",
			Instance: "wash-source-test",
		}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := logger.Close(); err != nil {
				t.Error(err)
			}
		})
		client := &washTestClient{failure: true}
		runtime := &Runtime{
			cfg: &venueconfig.RuntimeConfig{
				InstanceID:       7,
				Revision:         3,
				Template:         domain.StrategyTemplateWashTrade,
				OperationTimeout: time.Second,
				Venue: &common.VenuePolicy{
					QuantityFraction: "0.1",
					Profiles: []*common.VenueProfilePolicy{{
						Symbol:       "BTCUSDT",
						PriceTick:    "0.1",
						QuantityStep: "0.001",
					}},
					SyntheticTape: &config.SyntheticTape{
						Enabled:         true,
						DevelopmentOnly: true,
						Mode:            config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE,
						Interval:        durationpb.New(0),
						Profiles: []*config.SyntheticTapeProfile{{
							Symbol:   "BTCUSDT",
							Quantity: "0.01",
						}},
					},
				},
			},
			logger:  logger,
			client:  &atomic.Pointer[gatewaySession]{},
			closing: &atomic.Bool{},
		}
		runtime.client.Store(&gatewaySession{Client: client})
		runtime.washControl.Store(&washControl{
			active:   true,
			observed: time.Now(),
		})
		now := time.Now().UnixMilli()
		trade := &common.VenueTrade{
			Symbol:       "BTCUSDT",
			Source:       native.Source,
			Segment:      native.Segment,
			Side:         string(domain.SideSell),
			Price:        "100.43",
			Quantity:     "0.079",
			EventId:      "40",
			EventAtMs:    now,
			TradeAtMs:    now,
			ReceivedAtMs: now,
			ValidUntilMs: now + native.TradeAge.Milliseconds(),
			Publisher:    &common.VenueAuthority{AccountId: native.TradeKey("BTCUSDT")},
		}
		input := &pb.WashTradeRequest{
			StrategyInstanceId: 7,
			StrategyRevision:   3,
			Symbol:             "BTCUSDT",
			Side:               string(domain.SideSell),
			Price:              "100.5",
			Quantity:           "0.008",
			Source:             &pb.WashTradeRequest_Trade{Trade: trade},
		}
		ctx := washTestContext(t.Context())
		response, err := runtime.WashTrade(ctx, input)
		if err != nil || response.GetState() != native.SyntheticUnknown || client.calls != 1 {
			t.Fatalf("first source result = %v %v", response, err)
		}
		if _, err := runtime.WashTrade(ctx, input); status.Code(err) != codes.AlreadyExists || client.calls != 1 {
			t.Fatalf("UNKNOWN source was replayed: %v calls=%d", err, client.calls)
		}
		next := proto.CloneOf(input)
		next.GetTrade().EventId = "41"
		client.failure = false
		response, err = runtime.WashTrade(ctx, next)
		if err != nil || response.GetState() != native.SyntheticPublished || client.calls != 2 {
			t.Fatalf("new source artificially throttled: %v %v", response, err)
		}
		input.GetTrade().EventId = "39"
		if _, err := runtime.WashTrade(ctx, input); status.Code(err) != codes.AlreadyExists || client.calls != 2 {
			t.Fatalf("reversed source reached SDK: %v", err)
		}
	})
}
