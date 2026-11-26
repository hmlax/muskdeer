//go:build integration

package venue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type countedQuoteClient struct {
	*directQuoteClient
	reads      atomic.Int64
	orderReads atomic.Int64
}

func (c *countedQuoteClient) Read(ctx context.Context, scope gateway.Scope) (*gateway.Observation, error) {
	c.reads.Add(1)
	return c.directQuoteClient.Read(ctx, scope)
}

func (c *countedQuoteClient) ReadOrders(ctx context.Context, scope gateway.Scope) ([]*common.VenueOrder, error) {
	c.orderReads.Add(1)
	return c.directQuoteClient.ReadOrders(ctx, scope)
}

func TestFailedQuoteProtectionReusesFreshReadAndInvalidatesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	runtime := setup.runtime
	runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	runtime.store.matching = true
	setup.profile.StrategyInstanceId = setup.instance.Id
	setup.client.snapshot.Orders = []*common.VenueOrder{{
		OrderId:          "9001",
		ClientId:         native.InstanceClientPrefix(setup.profile) + "cached-protection",
		Side:             string(domain.SideBuy),
		Price:            "99",
		Quantity:         "0.1",
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	}}
	client := &countedQuoteClient{directQuoteClient: &directQuoteClient{currentStateClient: setup.client}}
	runtime.client.Store(&gatewaySession{Client: client})
	forbidden := denyMakerOrderStorage(t, setup.db)
	ctx = withQuoteAttemptObservation(ctx)
	if _, err := runtime.refresh(ctx, setup.profile, setup.lease, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.protectionObservation(ctx, setup.profile); err != nil || client.reads.Load() != 1 {
		t.Fatalf("protection repeated a fresh observation: reads=%d err=%v", client.reads.Load(), err)
	}
	if err := runtime.cancelManaged(ctx, setup.profile); err != nil {
		t.Fatal(err)
	}
	if client.reads.Load() != 1 || client.orderReads.Load() != 0 || client.cancellations.Load() != 1 {
		t.Fatalf("controlled cancellation repeated SDK reads: full=%d orders=%d cancels=%d", client.reads.Load(), client.orderReads.Load(), client.cancellations.Load())
	}
	if cachedQuoteObservation(ctx, setup.profile) != nil {
		t.Fatal("a dispatched cancellation did not invalidate the preceding observation")
	}
	if _, err := runtime.protectionObservation(ctx, setup.profile); err != nil || client.reads.Load() != 2 {
		t.Fatalf("post-send protection failed to require a new observation: reads=%d err=%v", client.reads.Load(), err)
	}
	if forbidden.Load() != 0 {
		t.Fatal("quote protection accessed order persistence")
	}
}
