package himalaya

import (
	"errors"
	"testing"

	pb "gitlab.com/himalaya/lukla-go/pb"

	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestOrderObservationForCancellationDoesNotRequireTradingReadiness(t *testing.T) {
	t.Parallel()
	scope := Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}
	peer := &sdkPeer{
		openOrders: []*pb.OrderRow{
			{
				OrderId:  42,
				UserId:   scope.UserID,
				FundId:   scope.FundID,
				Symbol:   scope.Symbol,
				Side:     pb.Side_SIDE_BUY,
				Price:    "100",
				Quantity: 1,
				Status:   native.StatusOpen,
			},
		},
	}
	client := sdkClient(t, peer, &testAdmission{})
	if _, err := client.Read(t.Context(), scope); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("unready trading snapshot = %v, want rejected", err)
	}
	orders, err := client.ReadOrders(t.Context(), scope)
	if err != nil || len(orders) != 1 || orders[0].OrderId != "42" || orders[0].Quantity != "1" {
		t.Fatalf("controlled-cancel order observations = %v, %v; want order 42", orders, err)
	}
	request := CancelRequest{
		Scope:   scope,
		OrderID: 42,
	}
	digest, err := (Factory{}).CancelDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Cancel(t.Context(), CancelAdmission{
		ID:      1,
		OwnerID: "cancel-owner",
		Epoch:   1,
		Request: request,
		Digest:  digest,
	})
	peer.mu.Lock()
	cancels, placements := peer.cancelCalls, peer.calls
	peer.mu.Unlock()
	if err != nil || cancels != 1 || placements != 0 {
		t.Fatalf("controlled cancellation = %v, cancels=%d placements=%d; want nil/1/0", err, cancels, placements)
	}
}
