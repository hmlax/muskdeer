package venue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// Any trading call through the embedded nil interfaces panics. Inspection must
// use only ReadGrid, with no database, existing strategy, or financial effect.
type accountInspectionClient struct {
	gateway.Client
	gateway.LongGridClient
	reads  int
	closed bool
}

func (c *accountInspectionClient) Ping(context.Context) error  { return nil }
func (c *accountInspectionClient) Close(context.Context) error { c.closed = true; return nil }

type accountInspectionFactory struct {
	gateway.Factory
	client gateway.Client
}

func (f accountInspectionFactory) Dial(context.Context, Config, SubmissionGuard) (gateway.Client, error) {
	return f.client, nil
}

func TestInspectionSessionWithoutStrategiesJoinsBeforeClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &accountInspectionClient{}
		parent := &Instances{
			client:     &atomic.Pointer[gatewaySession]{},
			closing:    &atomic.Bool{},
			connection: &config.VenueConnection{Target: "test-only"},
			gateway:    accountInspectionFactory{client: client},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- parent.run(ctx) }()
		synctest.Wait()
		_, err := parent.InspectAccount(washTestContext(ctx), &common.VenueScope{
			UserId: 1,
			FundId: 2,
			Symbol: "BTCUSDT",
		})
		if err != nil || client.reads != 1 {
			t.Fatalf("inspection without instances: %v", err)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("inspection session exit: %v", err)
		}
		if !client.closed || !parent.closing.Load() {
			t.Fatal("inspection session outlived its owner")
		}
	})
}

func (c *accountInspectionClient) ReadGrid(_ context.Context, scope gateway.Scope) (*gateway.Observation, error) {
	c.reads++
	return &gateway.Observation{Snapshot: &common.VenueSnapshot{Scope: &common.VenueScope{
		UserId: scope.UserID,
		FundId: scope.FundID,
		Symbol: scope.Symbol,
	}}}, nil
}

func TestInspectionReadsUnconfiguredFundWithoutDatabaseOrTrading(t *testing.T) {
	t.Parallel()
	client := &accountInspectionClient{}
	parent := &Instances{
		client:  &atomic.Pointer[gatewaySession]{},
		closing: &atomic.Bool{},
	}
	parent.client.Store(&gatewaySession{Client: client})
	scope := &common.VenueScope{
		UserId: 1,
		FundId: 2,
		Symbol: "BTCUSDT",
	}
	if _, err := parent.InspectAccount(t.Context(), scope); status.Code(err) != codes.PermissionDenied || client.reads != 0 {
		t.Fatalf("unverified inspection: %v, reads=%d", err, client.reads)
	}
	view, err := parent.InspectAccount(washTestContext(t.Context()), scope)
	if err != nil || view.GetScope().GetFundId() != 2 || client.reads != 1 {
		t.Fatalf("unconfigured inspection: %v, %v, reads=%d", view, err, client.reads)
	}
	view.Scope.FundId = 999
	if scope.FundId != 2 {
		t.Fatal("inspection response aliased caller input")
	}
	parent.closing.Store(true)
	if _, err := parent.InspectAccount(washTestContext(t.Context()), scope); status.Code(err) != codes.Unavailable || client.reads != 1 {
		t.Fatalf("inspection continued during close: %v", err)
	}
}
