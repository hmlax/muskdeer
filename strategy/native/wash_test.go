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
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type washMarket struct {
	market.MarketClient
	incoming chan *common.VenueTrade
}
type washTradeStream struct {
	grpc.ClientStream
	ctx      context.Context
	incoming <-chan *common.VenueTrade
}

func (c *washMarket) SubscribeTrades(ctx context.Context, _ *market.TradeRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[common.VenueTrade], error) {
	return &washTradeStream{ctx: ctx, incoming: c.incoming}, nil
}
func (s *washTradeStream) Recv() (*common.VenueTrade, error) {
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case v := <-s.incoming:
		return v, nil
	}
}
func TestWashSubscriptionCalculatesLatestTradeAndJoinsReceiver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger, err := logging.New(t.Context(), nil, logging.Identity{Service: "strategy", Instance: "wash-test"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := logger.Close(); err != nil {
				t.Error(err)
			}
		})
		cfg := &venueconfig.RuntimeConfig{
			InstanceID: 7, Revision: 3, OperationTimeout: time.Second,
			Venue: &common.VenuePolicy{
				QuantityFraction: "0.1", Profiles: []*common.VenueProfilePolicy{{Symbol: "BTCUSDT", PriceTick: "0.1", QuantityStep: "0.001"}},
				SyntheticTape: &config.SyntheticTape{
					Enabled: true, DevelopmentOnly: true, Mode: config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE,
					Interval: durationpb.New(100 * time.Millisecond), Profiles: []*config.SyntheticTapeProfile{{Symbol: "BTCUSDT", Quantity: "0.01"}},
				},
			},
		}
		marketClient := &washMarket{incoming: make(chan *common.VenueTrade, 10)}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sent := make(chan *execution.WashTradeRequest, 10)
		done := make(chan error, 1)
		go func() {
			done <- subscribeWash(ctx, cfg, "BTCUSDT", marketClient, func(_ context.Context, v *execution.WashTradeRequest) error { sent <- v; return nil }, logger)
		}()
		synctest.Wait()
		for _, id := range []string{"40", "41", "42"} {
			now := time.Now().UnixMilli()
			marketClient.incoming <- &common.VenueTrade{
				Symbol: "BTCUSDT", Source: values.Source, Segment: values.Segment, EventId: id, Side: string(domain.SideSell), Price: "100.43", Quantity: "0.079",
				EventAtMs: now, TradeAtMs: now, ReceivedAtMs: now, ValidUntilMs: now + values.TradeAge.Milliseconds(),
				Publisher: &common.VenueAuthority{AccountId: values.TradeKey("BTCUSDT")},
			}
		}
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if len(sent) != 1 {
			t.Fatalf("published %d events, want one latest source", len(sent))
		}
		request := <-sent
		if request.StrategyInstanceId != 7 || request.StrategyRevision != 3 || request.GetTrade().EventId != "42" || request.Price != "100.5" || request.Quantity != "0.007" {
			t.Fatalf("publication = %v", request)
		}
		marketClient.incoming <- request.GetTrade()
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if len(sent) != 0 {
			t.Fatal("same source replayed within subscription")
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("close = %v", err)
		}
	})
}
