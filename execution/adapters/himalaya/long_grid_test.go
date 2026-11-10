package himalaya

import (
	"errors"
	"testing"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
)

func TestLongGridSDKForcesCrossIOCAndSellReduceOnly(t *testing.T) {
	peer := &sdkPeer{}
	client := sdkClient(t, peer, nil)
	input := gateway.GridRequest{
		Scope: Scope{
			UserID: 20000,
			FundID: 20000,
			Symbol: "BTCUSDT",
		},
		ClientID: "grid-test-1",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.RequireFromString("0.1"),
		Leverage: 10,
	}
	if _, err := client.SubmitGrid(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if peer.received.ReduceOnly || peer.received.Side != pb.Side_SIDE_BUY || peer.received.OrderKind != pb.OrderKind_ORDER_KIND_LIMIT || peer.received.TimeInForce != pb.TimeInForce_TIME_IN_FORCE_IOC || peer.received.PositionType != pb.PositionType_POSITION_TYPE_CROSS || peer.received.Leverage != 10 {
		t.Fatalf("initial request: %v", peer.received)
	}
	input.Side = domain.SideSell
	if _, err := client.SubmitGrid(t.Context(), input); !errors.Is(err, ErrInvalidSubmit) {
		t.Fatal("sell opened a short")
	}
	input.ReduceOnly = true
	input.PositionID = "actual-long-position"
	input.Market = true
	input.Price = decimal.Zero
	if _, err := client.SubmitGrid(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if !peer.received.ReduceOnly || peer.received.PositionId != input.PositionID || peer.received.OrderKind != pb.OrderKind_ORDER_KIND_MARKET || peer.received.Quantity != "0.1" {
		t.Fatalf("stop request: %v", peer.received)
	}
	if peer.count() != 2 {
		t.Fatalf("SDK calls %d", peer.count())
	}
}

func TestLongGridSDKReadsExactPartialFillIncludingMarketOrder(t *testing.T) {
	peer := &sdkPeer{historyOrder: &pb.OrderRow{
		OrderId:          42,
		UserId:           20000,
		FundId:           20000,
		Symbol:           "BTCUSDT",
		ClientId:         "grid-test-1",
		Quantity:         1000,
		ExecutedQuantity: 400,
		ExecutedNotional: "40.4",
		Price:            "0",
		Status:           "CANCELED",
		Side:             pb.Side_SIDE_SELL,
	}}
	client := sdkClient(t, peer, nil)
	order, err := client.FindGridOrder(t.Context(), Scope{
		UserID: 20000,
		FundID: 20000,
		Symbol: "BTCUSDT",
	}, "grid-test-1", 42, 3)
	if err != nil || order.ExecutedQuantity != "0.4" || order.ExecutedNotional != "40.4" || !order.ReduceOnly {
		t.Fatalf("fill fact: %+v %v", order, err)
	}
	if peer.historyRequest.GetBefore().GetOrderId() != 43 || peer.historyRequest.Limit != 1 {
		t.Fatal("ACK lookup scanned unrelated history")
	}
}

func TestLongGridSDKDoesNotRetryLostReply(t *testing.T) {
	peer := &sdkPeer{fail: true}
	client := sdkClient(t, peer, nil)
	_, err := client.SubmitGrid(t.Context(), gateway.GridRequest{
		Scope: Scope{
			UserID: 20000,
			FundID: 20000,
			Symbol: "BTCUSDT",
		},
		ClientID: "grid-once-test",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.RequireFromString("0.1"),
		Leverage: 10,
	})
	if err == nil || !errors.Is(err, domain.ErrOutcomeUnknown) || peer.count() != 1 {
		t.Fatalf("lost reply replayed: calls=%d, err=%v", peer.count(), err)
	}
}
