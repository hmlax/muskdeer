//go:build integration

package migrate

import (
	"context"
	"net"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type nativeMarketFixture struct {
	himalayav1.UnimplementedMarketServer
	authority *commonv1.HimalayaAuthority
	stop      atomic.Bool
}

func (m *nativeMarketFixture) GetBook(context.Context, *himalayav1.BookRequest) (*commonv1.HimalayaBook, error) {
	if m.stop.Load() {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return &commonv1.HimalayaBook{
		Symbol:       "BTCUSDT",
		Source:       native.Source,
		Segment:      native.Segment,
		Sequence:     1,
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Publisher:    proto.CloneOf(m.authority),
		Bids:         []*commonv1.HimalayaLevel{{Price: "99.9", Quantity: "1"}},
		Asks:         []*commonv1.HimalayaLevel{{Price: "100.1", Quantity: "1"}},
	}, nil
}

type nativeSDKFixture struct {
	pb.UnimplementedOrderServer
	mu           sync.Mutex
	orders       map[uint64]*pb.OrderRow
	next         uint64
	historyDelay time.Duration
	synthetic    []*pb.WashTradeRequest
}

func startNativeSDK(t *testing.T, fixture *nativeSDKFixture) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterOrderServer(server, fixture)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	return listener.Addr().String()
}

func (*nativeSDKFixture) Ping(context.Context, *pb.PingRequest) (*pb.PingResponse, error) {
	return &pb.PingResponse{}, nil
}
func (*nativeSDKFixture) GetMarket(context.Context, *pb.GetMarketRequest) (*pb.GetMarketResponse, error) {
	return &pb.GetMarketResponse{
		Market: &pb.MarketView{
			Symbol:                  "BTCUSDT",
			EffectiveQuotePrecision: 2,
			EffectiveBasePrecision:  3,
			PriceTickTicks:          10,
			BaseIncrement:           1,
			BaseMinSize:             1,
			QuoteMinSize:            500000000,
			Status:                  1,
			MaxLeverage:             10,
			MakerFeeRate:            "0.0002",
			TakerFeeRate:            "0.0005",
		},
		Health: &pb.MarketHealth{
			Ok:             true,
			MarkPriceTicks: 10000,
		},
	}, nil
}
func (*nativeSDKFixture) GetAccount(_ context.Context, r *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	return &pb.GetAccountResponse{
		UserId:    r.UserId,
		FundId:    r.FundId,
		Available: "1000000",
	}, nil
}
func (*nativeSDKFixture) ListPositions(context.Context, *pb.ListPositionsRequest) (*pb.ListPositionsResponse, error) {
	return &pb.ListPositionsResponse{}, nil
}
func (s *nativeSDKFixture) Place(_ context.Context, r *pb.PlaceRequest) (*pb.PlaceOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	quantity, err := decimal.NewFromString(r.Quantity)
	if err != nil {
		return nil, err
	}
	s.orders[s.next] = &pb.OrderRow{
		OrderId:  s.next,
		UserId:   r.UserId,
		FundId:   r.FundId,
		Symbol:   r.Symbol,
		ClientId: r.ClientId,
		Price:    r.Price,
		Quantity: quantity.Shift(3).BigInt().Uint64(),
		Side:     r.Side,
		Status:   native.StatusOpen,
	}
	return &pb.PlaceOrderResponse{
		OrderId:  s.next,
		ClientId: r.ClientId,
	}, nil
}
func (s *nativeSDKFixture) CancelOrder(_ context.Context, r *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if order := s.orders[r.OrderId]; order != nil {
		order.Status = native.StatusCanceled
	}
	return &pb.CancelOrderResponse{Success: true}, nil
}
func (s *nativeSDKFixture) ListOrders(ctx context.Context, r *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	if r.History && s.historyDelay > 0 {
		if err := native.Wait(ctx, s.historyDelay); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*pb.OrderRow
	for _, order := range s.orders {
		if order.UserId != r.UserId || order.FundId != r.FundId {
			continue
		}
		if r.History {
			if !native.Terminal(order.Status) || r.Before != nil && order.OrderId >= r.Before.OrderId {
				continue
			}
		} else if !native.Active(order.Status) {
			continue
		}
		result = append(result, proto.CloneOf(order))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OrderId > result[j].OrderId })
	if r.Limit > 0 && len(result) > int(r.Limit) {
		result = result[:r.Limit]
	}
	return &pb.ListOrdersResponse{Orders: result}, nil
}
func (s *nativeSDKFixture) active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, order := range s.orders {
		if native.Active(order.Status) {
			count++
		}
	}
	return count
}

func (s *nativeSDKFixture) WashTrade(ctx context.Context, request *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if auth, protocol := md.Get("authorization"), md.Get("x-orderbook-protocol"); len(auth) != 1 || auth[0] != "Bearer test-token" || len(protocol) != 1 || protocol[0] != "2" {
		return nil, status.Error(codes.Unauthenticated, "SDK test authentication mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.synthetic = append(s.synthetic, proto.CloneOf(request))
	return &pb.WashTradeResponse{
		TradeId:     "wash-test-" + strconv.Itoa(len(s.synthetic)),
		Price:       request.Price,
		Quantity:    request.Quantity,
		TimestampMs: uint64(time.Now().UnixMilli()),
	}, nil
}

func (s *nativeSDKFixture) syntheticCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.synthetic)
}
