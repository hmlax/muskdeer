package venue

import (
	"context"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type makerObservationClient struct {
	gateway.Client
	reads    atomic.Int64
	snapshot *common.VenueSnapshot
}

func (c *makerObservationClient) Read(context.Context, gateway.Scope) (*gateway.Observation, error) {
	c.reads.Add(1)
	return &gateway.Observation{Snapshot: proto.CloneOf(c.snapshot)}, nil
}

func TestMakerSnapshotDoesNotCompeteWithQuoteWriter(t *testing.T) {
	t.Parallel()
	runtime, client, profile := makerObservationRuntime()
	gate := runtime.gates[AccountID(1, 2)]
	gate <- struct{}{} // A quote writer owns the existing financial admission.
	snapshot, err := runtime.GetSnapshot(washTestContext(t.Context()), client.snapshot.Scope)
	if err != nil || snapshot.GetScope().GetSymbol() != profile.Symbol || client.reads.Load() != 1 {
		t.Fatalf("snapshot while quote writer owns fund: %v, reads=%d", err, client.reads.Load())
	}
	if len(gate) != 1 {
		t.Fatal("read-only snapshot changed writer admission")
	}
	snapshot.Scope.Symbol = "CHANGED"
	if client.snapshot.Scope.Symbol != profile.Symbol {
		t.Fatal("snapshot mutated the observation source")
	}
}

func TestMakerObservationPreservesLifecycleAuthenticationAndLegacyAdmission(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"not_started", "closing", "unauthenticated", "legacy_writer"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			runtime, client, _ := makerObservationRuntime()
			ctx := washTestContext(t.Context())
			want := codes.Unavailable
			switch kind {
			case "not_started":
				runtime.client.Store(nil)
			case "closing":
				runtime.closing.Store(true)
			case "unauthenticated":
				ctx, want = t.Context(), codes.PermissionDenied
			case "legacy_writer":
				runtime.cfg.InstanceID = 0
				runtime.gates[AccountID(1, 2)] <- struct{}{}
				want = codes.ResourceExhausted
			}
			_, err := runtime.GetSnapshot(ctx, client.snapshot.Scope)
			if status.Code(err) != want || client.reads.Load() != 0 {
				t.Fatalf("GetSnapshot(%s) = %v, reads=%d", kind, err, client.reads.Load())
			}
		})
	}
}

func makerObservationRuntime() (*Runtime, *makerObservationClient, *common.VenueProfilePolicy) {
	profile := &common.VenueProfilePolicy{
		UserId:       1,
		FundId:       2,
		Symbol:       "BTCUSDT",
		PriceTick:    "0.1",
		QuantityStep: "0.001",
		Leverage:     10,
	}
	client := &makerObservationClient{snapshot: &common.VenueSnapshot{
		Scope: &common.VenueScope{
			UserId: 1,
			FundId: 2,
			Symbol: "BTCUSDT",
		},
		Market: &common.VenueMarket{
			PriceTick:       profile.PriceTick,
			QuantityStep:    profile.QuantityStep,
			MaximumLeverage: profile.Leverage,
		},
	}}
	gate := make(chan struct{}, 1)
	runtime := &Runtime{
		cfg: &venueconfig.RuntimeConfig{
			InstanceID: 1,
			Template:   domain.StrategyTemplateMarketMaker,
			Venue:      &common.VenuePolicy{Profiles: []*common.VenueProfilePolicy{profile}},
		},
		gates:   map[string]chan struct{}{AccountID(1, 2): gate},
		client:  &atomic.Pointer[gatewaySession]{},
		closing: &atomic.Bool{},
	}
	runtime.client.Store(&gatewaySession{Client: client})
	return runtime, client, profile
}
