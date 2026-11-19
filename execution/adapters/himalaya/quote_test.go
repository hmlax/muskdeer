package himalaya

import (
	"context"
	"errors"
	"testing"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func directOrder() gateway.QuoteRequest {
	order := sdkOrder()
	return gateway.QuoteRequest{
		Scope: gateway.Scope{
			UserID: order.UserID,
			FundID: order.FundID,
			Symbol: order.Symbol,
		},
		ClientID: order.ClientID,
		Side:     order.Side,
		Price:    order.Price,
		Quantity: order.Quantity,
		Leverage: order.Leverage,
	}
}

func TestDirectMakerSDKUsesNoPersistenceIdentityOrGuard(t *testing.T) {
	t.Parallel()
	server := &sdkPeer{}
	client := sdkClient(t, server, nil)
	request := directOrder()
	reply, err := client.SubmitQuote(t.Context(), request)
	if err != nil || reply.GetOrderID() != 42 || server.count() != 1 {
		t.Fatalf("direct quote = %v, %v", reply, err)
	}
	if server.received.ClientId != request.ClientID || server.received.Price != request.Price.String() {
		t.Fatal("direct quote lost wire identity/decimal")
	}
	if err := client.CancelQuote(t.Context(), gateway.CancelRequest{
		Scope:   request.Scope,
		OrderID: reply.OrderID,
	}); err != nil {
		t.Fatalf("direct cancellation = %v", err)
	}
}

func TestDirectMakerSDKReplyLossDoesNotRetry(t *testing.T) {
	t.Parallel()
	server := &sdkPeer{fail: true}
	client := sdkClient(t, server, nil)
	if _, err := client.SubmitQuote(t.Context(), directOrder()); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("reply loss = %v", err)
	}
	if server.count() != 1 {
		t.Fatalf("direct quote sent %d times", server.count())
	}
}

func TestDirectCancellationBeforeDispatchIsDistinguishable(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"invalid", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			server := &sdkPeer{}
			client := sdkClient(t, server, nil)
			request := gateway.CancelRequest{
				Scope:   directOrder().Scope,
				OrderID: 42,
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "invalid" {
				request.OrderID = 0
			} else {
				cancel()
			}
			err := client.CancelQuote(ctx, request)
			if !errors.Is(err, gateway.ErrNotDispatched) || errors.Is(err, domain.ErrOutcomeUnknown) {
				t.Fatalf("pre-dispatch failure became an ambiguous cancel: %v", err)
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if server.cancelCalls != 0 {
				t.Fatal("pre-dispatch failure reached SDK")
			}
		})
	}
}
