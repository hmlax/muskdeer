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
	peer := &sdkPeer{rejectPlace: true}
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
	if peer.gridOpen == nil || peer.gridOpen.Side != pb.Side_SIDE_BUY || peer.gridOpen.OrderKind != pb.OrderKind_ORDER_KIND_LIMIT || peer.gridOpen.TimeInForce != pb.TimeInForce_TIME_IN_FORCE_IOC || peer.gridOpen.PositionType != pb.PositionType_POSITION_TYPE_CROSS || peer.gridOpen.Leverage != 10 || peer.gridOpen.MakerFeeRatePpm != 200 || peer.gridOpen.TakerFeeRatePpm != 500 {
		t.Fatalf("initial request: %v", peer.gridOpen)
	}
	input.Side = domain.SideSell
	if _, err := client.SubmitGrid(t.Context(), input); !errors.Is(err, ErrInvalidSubmit) {
		t.Fatal("sell opened a short")
	}
	input.ReduceOnly = true
	input.PositionID = "actual-long-position"
	if _, err := client.SubmitGrid(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if peer.gridClose == nil || peer.gridClose.PositionId != input.PositionID || peer.gridClose.OrderKind != pb.OrderKind_ORDER_KIND_LIMIT || peer.gridClose.TimeInForce != pb.TimeInForce_TIME_IN_FORCE_IOC {
		t.Fatalf("profit close request: %v", peer.gridClose)
	}
	input.Market = true
	input.Price = decimal.Zero
	if _, err := client.SubmitGrid(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if peer.gridClose == nil || peer.gridClose.PositionId != input.PositionID || peer.gridClose.OrderKind != pb.OrderKind_ORDER_KIND_MARKET || peer.gridClose.Quantity != "0.1" || peer.gridClose.TimeInForce != pb.TimeInForce_TIME_IN_FORCE_IOC {
		t.Fatalf("stop request: %v", peer.gridClose)
	}
	if peer.count() != 3 {
		t.Fatalf("SDK calls %d", peer.count())
	}
}

func TestGridFeeConversionKeepsRebatesAndRejectsTruncation(t *testing.T) {
	t.Parallel()
	ppm, err := gridFeePPM("-0.0002")
	if err != nil || ppm != -200 {
		t.Fatalf("gridFeePPM(rebate) = %d, %v; want -200, nil", ppm, err)
	}
	if _, err := gridFeePPM("0.0000001"); err == nil {
		t.Fatal("sub-ppm fee was silently rounded")
	}
	if _, err := gridFeePPM(""); err == nil {
		t.Fatal("missing venue fee was silently changed to zero")
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
