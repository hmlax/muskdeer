package himalaya

import (
	"testing"

	pb "gitlab.com/himalaya/lukla-go/pb"
)

func TestKnownNativeIDLookupSeeksTerminalHistoryWithoutAssumingClientDedup(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{historyOrder: &pb.OrderRow{
		OrderId:  42,
		UserId:   10001,
		FundId:   10001,
		Symbol:   "BTCUSDT",
		Side:     pb.Side_SIDE_BUY,
		Price:    "100",
		Quantity: 2000,
		Status:   "CANCELED",
	}}
	client := sdkClient(t, peer, nil)
	order, err := client.FindKnown(t.Context(), Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}, 42, 3)
	if err != nil || order.OrderId != "42" || order.Quantity != "2" || !peer.historyRequest.History || peer.historyRequest.Before.OrderId != 43 || peer.historyRequest.Limit != 1 {
		t.Fatalf("native identity lookup lost its exact cursor/scope: %v %v", order, err)
	}
}

func TestSDKCancelUsesThePersistedNativeIDOnce(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, &testAdmission{})
	request := CancelRequest{
		Scope: Scope{
			UserID: 10001,
			FundID: 10001,
			Symbol: "BTCUSDT",
		},
		OrderID: 42,
	}
	_, digest, err := cancelInput(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Cancel(t.Context(), CancelAdmission{
		ID:      1,
		OwnerID: "execution-test",
		Epoch:   1,
		Request: request,
		Digest:  digest,
	}); err != nil {
		t.Fatal(err)
	}
	if peer.cancelCalls != 1 || peer.count() != 0 {
		t.Fatal("cancel repeated or placed an opening order")
	}
}
