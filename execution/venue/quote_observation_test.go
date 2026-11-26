package venue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestFailedQuoteCanReuseOnlyItsOwnFreshUnchangedObservation(t *testing.T) {
	profile := &common.VenueProfilePolicy{
		UserId:       1,
		FundId:       2,
		Symbol:       "BTCUSDT",
		PriceTick:    "0.1",
		QuantityStep: "0.001",
		Leverage:     10,
	}
	now := time.Now().UnixMilli()
	snapshot := &common.VenueSnapshot{
		Scope: &common.VenueScope{
			UserId: 1,
			FundId: 2,
			Symbol: "BTCUSDT",
		},
		Market: &common.VenueMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MaximumLeverage: 100,
		},
		StartedAtMs:  now,
		ObservedAtMs: now,
		ValidUntilMs: now + native.SnapshotAge.Milliseconds(),
	}
	for _, kind := range []string{"fresh", "expired", "wrong_fund", "changed_tick", "write_attempted", "later_observation"} {
		t.Run(kind, func(t *testing.T) {
			ctx := withQuoteAttemptObservation(t.Context())
			value := proto.CloneOf(snapshot)
			wanted := kind == "fresh" || kind == "later_observation"
			switch kind {
			case "expired":
				value.ValidUntilMs = now - 1
			case "wrong_fund":
				value.Scope.FundId++
			case "changed_tick":
				value.Market.PriceTick = "1"
			}
			rememberQuoteObservation(ctx, value)
			if kind == "write_attempted" || kind == "later_observation" {
				markQuoteObservationDirty(ctx)
			}
			if kind == "later_observation" {
				rememberQuoteObservation(ctx, value)
			}
			if got := cachedQuoteObservation(ctx, profile); (got != nil) != wanted {
				t.Fatalf("cached observation(%s) eligible=%t, want %t", kind, got != nil, wanted)
			}
			client := &quoteObservationClient{snapshot: snapshot}
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: 1,
					Template:   domain.StrategyTemplateMarketMaker,
				},
				client: &atomic.Pointer[gatewaySession]{},
			}
			runtime.client.Store(&gatewaySession{Client: client})
			if _, err := runtime.protectionObservation(ctx, profile); err != nil {
				t.Fatal(err)
			}
			calls := int64(1)
			if wanted {
				calls = 0
			}
			if client.calls.Load() != calls {
				t.Fatalf("protection(%s) read SDK %d times, want %d", kind, client.calls.Load(), calls)
			}
		})
	}
	if cachedQuoteObservation(context.Background(), profile) != nil {
		t.Fatal("another attempt borrowed an observation")
	}
}

type quoteObservationClient struct {
	gateway.Client
	snapshot *common.VenueSnapshot
	calls    atomic.Int64
}

func (c *quoteObservationClient) Read(context.Context, gateway.Scope) (*gateway.Observation, error) {
	c.calls.Add(1)
	return &gateway.Observation{Snapshot: proto.CloneOf(c.snapshot)}, nil
}
