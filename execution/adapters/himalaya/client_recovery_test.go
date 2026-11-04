package himalaya

import (
	"errors"
	"testing"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestClientHistoryRequiresPositiveUnambiguousScopedFacts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"match", "missing", "duplicate", "foreign_scope"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			order := &pb.OrderRow{
				OrderId:  42,
				UserId:   10001,
				FundId:   10001,
				Symbol:   "BTCUSDT",
				ClientId: "deer-test-lost-reply",
				Side:     pb.Side_SIDE_BUY,
				Price:    "100",
				Quantity: 1000,
				Status:   native.StatusCanceled,
			}
			peer := &sdkPeer{historyOrders: []*pb.OrderRow{order}}
			switch kind {
			case "missing":
				order.ClientId = "unrelated-native-request"
			case "duplicate":
				other := proto.CloneOf(order)
				other.OrderId++
				peer.historyOrders = append(peer.historyOrders, other)
			case "foreign_scope":
				order.FundId++
			}
			client := sdkClient(t, peer, nil)
			found, err := client.FindClientOrders(t.Context(), Scope{
				UserID: 10001,
				FundID: 10001,
				Symbol: "BTCUSDT",
			}, []string{"deer-test-lost-reply"}, 3)
			switch kind {
			case "duplicate", "foreign_scope":
				if !errors.Is(err, ErrInvalidSnapshot) || len(found) != 0 {
					t.Fatalf("FindClientOrders(%s) = %v, %v, want invalid snapshot without facts", kind, found, err)
				}
			case "missing":
				if err != nil || len(found) != 0 {
					t.Fatalf("FindClientOrders(absent) = %v, %v, want no terminal assertion", found, err)
				}
			default:
				if err != nil || len(found) != 1 || found[0].OrderId != "42" || found[0].Quantity != "1" {
					t.Fatalf("FindClientOrders(positive) = %v, %v, want exact native fact", found, err)
				}
			}
			if peer.count() != 0 {
				t.Fatal("history recovery submitted a native order")
			}
		})
	}
}
