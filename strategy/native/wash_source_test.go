package native

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hmlax/muskdeer/deer/logging"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func sourceWashConfig() *venueconfig.RuntimeConfig {
	return &venueconfig.RuntimeConfig{
		InstanceID:       7,
		Revision:         3,
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
	}
}

func sourceWashTrade(id string) *common.VenueTrade {
	now := time.Now().UnixMilli()
	return &common.VenueTrade{
		Symbol:       "BTCUSDT",
		Source:       values.Source,
		Segment:      values.Segment,
		EventId:      id,
		Side:         string(domain.SideSell),
		Price:        "100.43",
		Quantity:     "0.079",
		EventAtMs:    now,
		TradeAtMs:    now,
		ReceivedAtMs: now,
		ValidUntilMs: now + values.TradeAge.Milliseconds(),
		Publisher:    &common.VenueAuthority{AccountId: values.TradeKey("BTCUSDT")},
	}
}

func sourceWashLogger(t *testing.T) *logging.Logger {
	t.Helper()
	logger, err := logging.New(t.Context(), nil, logging.Identity{
		Service:  "strategy",
		Instance: "source-wash-test",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	return logger
}

func TestSourceWashSendsEachEligibleEventWithoutTimerOrUnknownReplay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := sourceWashLogger(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		market := &washMarket{incoming: make(chan *common.VenueTrade, 10)}
		release := make(chan struct{})
		sent := make(chan *pb.WashTradeRequest, 10)
		done := make(chan error, 1)
		go func() {
			done <- subscribeWash(ctx, sourceWashConfig(), "BTCUSDT", market, func(_ context.Context, request *pb.WashTradeRequest) error {
				sent <- request
				if request.GetTrade().EventId == "40" {
					<-release
					return domain.ErrOutcomeUnknown
				}
				return nil
			}, logger)
		}()
		market.incoming <- sourceWashTrade("40")
		synctest.Wait()
		if len(sent) != 1 {
			t.Fatal("source event waited for a periodic timer")
		}
		small := sourceWashTrade("41")
		small.Quantity = "0.001"
		market.incoming <- small
		market.incoming <- sourceWashTrade("42")
		market.incoming <- sourceWashTrade("42")
		market.incoming <- sourceWashTrade("39")
		stale := sourceWashTrade("43")
		stale.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
		market.incoming <- stale
		market.incoming <- sourceWashTrade("44")
		synctest.Wait()
		close(release)
		synctest.Wait()
		if len(sent) != 4 {
			t.Fatalf("burst coalesced, stale event sent, or UNKNOWN replayed: %d requests", len(sent))
		}
		for _, id := range []string{"40", "41", "42", "44"} {
			request := <-sent
			quantity := "0.008"
			if id == "41" {
				quantity = "0.001"
			}
			if request.GetTrade().EventId != id || request.Price != "100.5" || request.Quantity != quantity || request.Side != string(domain.SideSell) {
				t.Fatalf("source order or decimal calculation changed: %v", request)
			}
		}
		market.incoming <- proto.CloneOf(sourceWashTrade("40"))
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		if len(sent) != 0 {
			t.Fatal("quiet source produced extra events")
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestSourceWashStopJoinsItsInFlightCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := sourceWashLogger(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		market := &washMarket{incoming: make(chan *common.VenueTrade, 2)}
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- subscribeWash(ctx, sourceWashConfig(), "BTCUSDT", market, func(work context.Context, _ *pb.WashTradeRequest) error {
				close(started)
				<-release
				return work.Err()
			}, logger)
		}()
		market.incoming <- sourceWashTrade("40")
		synctest.Wait()
		<-started
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("source worker returned before its call joined: %v", err)
		default:
		}
		close(release)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestSourceWashMinimumFilterIsCountedWithoutInflatingPublication(t *testing.T) {
	cfg := sourceWashConfig()
	stats := &washSourceStats{}
	calls := 0
	send := func(context.Context, *pb.WashTradeRequest) error { calls++; return domain.ErrBelowMarketMinimum }
	event := &washObservation{trade: sourceWashTrade("41")}
	publishWashSource(t.Context(), cfg, "BTCUSDT", event, send, stats)
	publishWashSource(t.Context(), cfg, "BTCUSDT", event, send, stats)
	if calls != 1 || stats.tooSmall != 1 || stats.duplicate != 1 || stats.submitted != 0 || stats.published != 0 || stats.unknown != 0 || stats.failed != 0 {
		t.Fatalf("filtered source stats are inaccurate: %+v", stats)
	}
	publishWashSource(t.Context(), cfg, "BTCUSDT", &washObservation{trade: sourceWashTrade("42")}, func(context.Context, *pb.WashTradeRequest) error { return nil }, stats)
	if stats.published != 1 || stats.submitted != 1 {
		t.Fatal("valid source event after a filter was blocked")
	}
}
