package himalaya

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestClientRefusesLegacyOrIncompleteGatewayWithoutAnyTradingCall(t *testing.T) {
	t.Parallel()
	for name, invalidate := range map[string]func(*himalayav1.Capabilities){
		"production profile":         func(c *himalayav1.Capabilities) { c.DevelopmentTestFunds = false },
		"no durable client identity": func(c *himalayav1.Capabilities) { c.DurableClientIdempotency = false },
		"no final owner fence":       func(c *himalayav1.Capabilities) { c.MatchingOwnerFencing = false },
		"no stable recovery query":   func(c *himalayav1.Capabilities) { c.QueryByClientId = false },
		"no post only":               func(c *himalayav1.Capabilities) { c.PostOnly = false },
		"wrong product":              func(c *himalayav1.Capabilities) { c.OneWayLinearPerpetual = false },
		"no lost owner cancellation": func(c *himalayav1.Capabilities) { c.OwnerExpiryCancel = false },
		"unknown protocol":           func(c *himalayav1.Capabilities) { c.ContractVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			peer := &gatewayPeer{capabilities: gatewayCapabilities()}
			invalidate(peer.capabilities)
			client, err := NewClient(t.Context(), peer)
			if !errors.Is(err, ErrUnsupportedGateway) || client != nil || peer.tradingCalls != 0 {
				t.Fatalf("unsupported gateway accepted: client=%v error=%v trades=%d", client, err, peer.tradingCalls)
			}
		})
	}
}

func TestClientRejectsWrongAuthorityAndDecimalBeforeDispatch(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*himalayav1.SubmitRequest){
		"wrong strategy account": func(r *himalayav1.SubmitRequest) { r.Strategy.Scope.FundId++ },
		"wrong strategy symbol":  func(r *himalayav1.SubmitRequest) { r.Strategy.Scope.Symbol = "ETHUSDT" },
		"wrong execution role":   func(r *himalayav1.SubmitRequest) { r.Execution.Scope.Kind = himalayav1.LeaseKind_LEASE_KIND_STRATEGY },
		"zero epoch":             func(r *himalayav1.SubmitRequest) { r.Execution.Epoch = 0 },
		"unspecified side":       func(r *himalayav1.SubmitRequest) { r.Side = himalayav1.Side_SIDE_UNSPECIFIED },
		"exponent input":         func(r *himalayav1.SubmitRequest) { r.Price = "1e2" },
		"zero size":              func(r *himalayav1.SubmitRequest) { r.Quantity = "0" },
		"taker order":            func(r *himalayav1.SubmitRequest) { r.PostOnly = false },
		"unbounded queue age":    func(r *himalayav1.SubmitRequest) { r.MaximumQueueAgeMs = 60_000 },
	} {
		t.Run(name, func(t *testing.T) {
			peer := &gatewayPeer{capabilities: gatewayCapabilities()}
			client, err := NewClient(t.Context(), peer)
			if err != nil {
				t.Fatal(err)
			}
			request := gatewaySubmit()
			change(request)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := client.Submit(ctx, request); !errors.Is(err, ErrInvalidRequest) || peer.tradingCalls != 0 {
				t.Fatalf("unsafe dispatch: error=%v trades=%d", err, peer.tradingCalls)
			}
		})
	}
}

func TestClientPreservesExactBusinessIdentityAndTreatsLostReplyAsUnknown(t *testing.T) {
	t.Parallel()
	lost := errors.New("injected reply loss")
	peer := &gatewayPeer{
		capabilities: gatewayCapabilities(),
		submitError:  lost,
	}
	client, err := NewClient(t.Context(), peer)
	if err != nil {
		t.Fatal(err)
	}
	request := gatewaySubmit()
	before := proto.CloneOf(request)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := client.Submit(ctx, request); !errors.Is(err, domain.ErrOutcomeUnknown) || !errors.Is(err, lost) {
		t.Fatalf("lost reply was not retained as UNKNOWN: %v", err)
	}
	if peer.tradingCalls != 1 || !proto.Equal(peer.submitted, request) || !proto.Equal(request, before) {
		t.Fatal("stable client identity or exact decimal input changed, or write was retried")
	}
}

type gatewayPeer struct {
	capabilities *himalayav1.Capabilities
	tradingCalls int
	submitError  error
	submitted    *himalayav1.SubmitRequest
}

func (p *gatewayPeer) Invoke(_ context.Context, method string, request, reply any, _ ...grpc.CallOption) error {
	switch method {
	case himalayav1.Gateway_GetCapabilities_FullMethodName:
		proto.Merge(reply.(*himalayav1.Capabilities), p.capabilities)
		return nil
	case himalayav1.Gateway_Submit_FullMethodName:
		p.tradingCalls++
		p.submitted = proto.CloneOf(request.(*himalayav1.SubmitRequest))
		return p.submitError
	default:
		return errors.New("unexpected RPC in contract test")
	}
}

func (*gatewayPeer) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("streaming is not used by the gateway adapter")
}

func gatewayCapabilities() *himalayav1.Capabilities {
	return &himalayav1.Capabilities{
		ContractVersion:          1,
		DevelopmentTestFunds:     true,
		DurableClientIdempotency: true,
		MatchingOwnerFencing:     true,
		QueryByClientId:          true,
		PostOnly:                 true,
		OneWayLinearPerpetual:    true,
		OwnerExpiryCancel:        true,
		Incarnation:              "matching-test",
	}
}

func gatewaySubmit() *himalayav1.SubmitRequest {
	return &himalayav1.SubmitRequest{
		UserId:       10001,
		FundId:       10001,
		Symbol:       "BTCUSDT",
		ClientId:     "deer-test-1",
		Side:         himalayav1.Side_SIDE_BUY,
		Price:        "100.100000000000000001",
		Quantity:     "0.001",
		Leverage:     10,
		PositionMode: himalayav1.PositionMode_POSITION_MODE_ONE_WAY,
		Execution: &himalayav1.Proof{
			Scope: &himalayav1.Scope{
				Kind:   himalayav1.LeaseKind_LEASE_KIND_EXECUTION,
				UserId: 10001,
				FundId: 10001,
			},
			Owner: "execution.first",
			Epoch: 1,
		},
		Strategy: &himalayav1.Proof{
			Scope: &himalayav1.Scope{
				Kind:   himalayav1.LeaseKind_LEASE_KIND_STRATEGY,
				UserId: 10001,
				FundId: 10001,
				Symbol: "BTCUSDT",
			},
			Owner: "strategy.first",
			Epoch: 1,
		},
		MaximumQueueAgeMs: 1000,
		RequestId:         "request-test-1",
		PostOnly:          true,
	}
}
