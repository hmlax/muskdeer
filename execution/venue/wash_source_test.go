package venue

import (
	"context"
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
		client.failure, client.filtered = false, true
		skipped := proto.CloneOf(next)
		skipped.GetTrade().EventId = "42"
		result, err := runtime.WashTrade(ctx, skipped)
		if err != nil || result.GetState() != native.SyntheticSkippedMinimum || client.calls != 3 {
			t.Fatalf("minimum filter became unknown or published: %v %v", result, err)
		}
		if _, err := runtime.WashTrade(ctx, skipped); status.Code(err) != codes.AlreadyExists || client.calls != 3 {
			t.Fatal("filtered event was replayed")
		}
		client.filtered = false
		fresh := proto.CloneOf(skipped)
		fresh.GetTrade().EventId = "43"
		if result, err := runtime.WashTrade(ctx, fresh); err != nil || result.GetState() != native.SyntheticPublished || client.calls != 4 {
			t.Fatalf("minimum filter blocked subsequent valid event: %v %v", result, err)
		}

		checkMinimumTargetAndMarketRPC(ctx, t, runtime, client, fresh)
	})
}

func checkMinimumTargetAndMarketRPC(ctx context.Context, t *testing.T, runtime *Runtime, client *washTestClient, fresh *pb.WashTradeRequest) {
	t.Helper()

	client.minimumNotional = "1"
	raised := proto.CloneOf(fresh)
	raised.GetTrade().EventId = "44"
	if _, err := runtime.WashTrade(ctx, raised); status.Code(err) != codes.InvalidArgument || client.calls != 4 {
		t.Fatal("unraised target bypassed venue minimum")
	}
	raised.Quantity = "0.01"
	if result, err := runtime.WashTrade(ctx, raised); err != nil || result.GetState() != native.SyntheticPublished || client.calls != 5 {
		t.Fatalf("strategy minimum target was not accepted: %v %v", result, err)
	}
	marketRequest := &pb.WashMarketRequest{
		StrategyInstanceId: 7,
		StrategyRevision:   3,
		Symbol:             "BTCUSDT",
	}
	if reply, err := runtime.GetWashMarket(ctx, marketRequest); err != nil || reply.GetMarket().GetMinimumNotional() != "1" || reply.GetValidUntilMs() <= time.Now().UnixMilli() {
		t.Fatalf("current minimum metadata unavailable: %v %v", reply, err)
	}
	if _, err := runtime.GetWashMarket(t.Context(), marketRequest); status.Code(err) != codes.PermissionDenied {
		t.Fatal("unauthorized rule query admitted")
	}
	marketRequest.StrategyRevision++
	if _, err := runtime.GetWashMarket(ctx, marketRequest); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("old configuration admitted")
	}
}
