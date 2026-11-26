package himalaya

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/trading/native"
)

type cancellationQueryPeer struct {
	sdkPeer
	accountReads  atomic.Int64
	positionReads atomic.Int64
	ordersBlocked bool
}

func (p *cancellationQueryPeer) GetAccount(context.Context, *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	p.accountReads.Add(1)
	return nil, status.Error(codes.Unavailable, "account observation unavailable")
}

func (p *cancellationQueryPeer) ListPositions(context.Context, *pb.ListPositionsRequest) (*pb.ListPositionsResponse, error) {
	p.positionReads.Add(1)
	return nil, status.Error(codes.Unavailable, "position observation unavailable")
}

func (p *cancellationQueryPeer) ListOrders(ctx context.Context, request *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	if p.ordersBlocked {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return p.sdkPeer.ListOrders(ctx, request)
}

func TestCancellationOrdersRemainReadableWhenAccountObservationFails(t *testing.T) {
	t.Parallel()
	scope := Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}
	peer := &cancellationQueryPeer{}
	peer.openOrders = []*pb.OrderRow{{
		OrderId:          42,
		UserId:           scope.UserID,
		FundId:           scope.FundID,
		Symbol:           scope.Symbol,
		ClientId:         "owned-cancellation",
		Side:             pb.Side_SIDE_BUY,
		Price:            "100.01",
		Quantity:         1234,
		ExecutedQuantity: 234,
		Status:           native.StatusPartial,
	}}
	client := sdkClient(t, peer, nil)
	orders, err := client.ReadOrders(t.Context(), scope)
	if err != nil || len(orders) != 1 || orders[0].Quantity != "1.234" || orders[0].ExecutedQuantity != "0.234" {
		t.Fatalf("cancellation observations depend on unavailable balances/positions: orders=%v err=%v", orders, err)
	}
	if peer.accountReads.Load() != 0 || peer.positionReads.Load() != 0 {
		t.Fatal("order-only query fetched balances or positions")
	}
	if _, err := client.Read(t.Context(), scope); status.Code(err) != codes.Unavailable {
		t.Fatalf("trading observation bypassed unavailable account state: %v", err)
	}
}

func TestCancellationOrderQueryKeepsScopePrecisionAndQuantityValidation(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"user", "fund", "symbol", "market", "precision", "executed"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			scope := Scope{
				UserID: 10001,
				FundID: 10001,
				Symbol: "BTCUSDT",
			}
			row := &pb.OrderRow{
				OrderId:  42,
				UserId:   scope.UserID,
				FundId:   scope.FundID,
				Symbol:   scope.Symbol,
				Side:     pb.Side_SIDE_BUY,
				Price:    "100",
				Quantity: 1,
				Status:   native.StatusOpen,
			}
			peer := &sdkPeer{openOrders: []*pb.OrderRow{row}}
			switch field {
			case "user":
				row.UserId++
			case "fund":
				row.FundId++
			case "symbol":
				row.Symbol = "ETHUSDT"
			case "market":
				peer.washMarket = &pb.MarketView{Symbol: "ETHUSDT"}
			case "precision":
				peer.washMarket = &pb.MarketView{
					Symbol:                 scope.Symbol,
					EffectiveBasePrecision: 19,
				}
			case "executed":
				row.ExecutedQuantity = 2
			}
			if _, err := sdkClient(t, peer, nil).ReadOrders(t.Context(), scope); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("invalid %s accepted: %v", field, err)
			}
		})
	}
}

func TestCancellationOrderQueryHonorsCallerDeadline(t *testing.T) {
	t.Parallel()
	peer := &cancellationQueryPeer{ordersBlocked: true}
	client := sdkClient(t, peer, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.ReadOrders(ctx, Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("order query ignored caller deadline: %v", err)
	}
}
