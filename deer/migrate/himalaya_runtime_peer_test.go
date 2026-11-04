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
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type nativeMarketFixture struct {
	himalayav1.UnimplementedMarketServer
	authority        *commonv1.HimalayaAuthority
	stop             atomic.Bool
	offset           atomic.Int64
	depth            uint32
	quantity         atomic.Int64
	renewal          sync.Mutex
	leases           domain.LeaseRepository
	lease            domain.LeaseToken
	tradeAuthority   *commonv1.HimalayaAuthority
	tradeLease       domain.LeaseToken
	tradeRequests    atomic.Int64
	tradeSequence    atomic.Int64
	tradeUnavailable atomic.Bool
}

func (m *nativeMarketFixture) GetTrade(ctx context.Context, _ *himalayav1.TradeRequest) (*commonv1.HimalayaTrade, error) {
	m.tradeRequests.Add(1)
	if m.tradeUnavailable.Load() || m.tradeAuthority == nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	publisher := proto.CloneOf(m.tradeAuthority)
	if m.leases != nil {
		m.renewal.Lock()
		token, err := m.leases.Renew(ctx, m.tradeLease, 10*time.Second)
		if err == nil {
			m.tradeLease = token
		}
		m.renewal.Unlock()
		if err != nil {
			return nil, status.Error(codes.Unavailable, native.MessageInvalid)
		}
		publisher = native.Authority(token)
	}
	now := time.Now()
	return &commonv1.HimalayaTrade{
		Symbol:       "BTCUSDT",
		Source:       native.Source,
		Segment:      native.Segment,
		EventId:      strconv.FormatInt(m.tradeSequence.Add(1), 10),
		Side:         string(domain.SideSell),
		Price:        "100.4",
		Quantity:     "0.7",
		EventAtMs:    now.UnixMilli(),
		TradeAtMs:    now.UnixMilli(),
		ReceivedAtMs: now.UnixMilli(),
		ValidUntilMs: now.Add(native.TradeAge).UnixMilli(),
		Publisher:    publisher,
	}, nil
}

func (m *nativeMarketFixture) GetBook(ctx context.Context, _ *himalayav1.BookRequest) (*commonv1.HimalayaBook, error) {
	if m.stop.Load() {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	publisher := proto.CloneOf(m.authority)
	if m.leases != nil {
		m.renewal.Lock()
		token, err := m.leases.Renew(ctx, m.lease, 10*time.Second)
		if err == nil {
			m.lease = token
		}
		m.renewal.Unlock()
		if err != nil {
			return nil, status.Error(codes.Unavailable, native.MessageInvalid)
		}
		publisher = native.Authority(token)
	}
	offset := decimal.NewFromInt(m.offset.Load()).Shift(-1)
	book := &commonv1.HimalayaBook{
		Symbol:       "BTCUSDT",
		Source:       native.Source,
		Segment:      native.Segment,
		Sequence:     1,
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Publisher:    publisher,
	}
	for level := range max(1, int(m.depth)) {
		distance := decimal.NewFromInt(int64(level)).Shift(-1)
		quantity := decimal.NewFromInt(max(1, m.quantity.Load())).String()
		book.Bids = append(book.Bids, &commonv1.HimalayaLevel{
			Price:    decimal.RequireFromString("99.9").Add(offset).Sub(distance).String(),
			Quantity: quantity,
		})
		book.Asks = append(book.Asks, &commonv1.HimalayaLevel{
			Price:    decimal.RequireFromString("100.1").Add(offset).Add(distance).String(),
			Quantity: quantity,
		})
	}
	return book, nil
}

type nativeSDKFixture struct {
	pb.UnimplementedOrderServer
	mu                sync.Mutex
	orders            map[uint64]*pb.OrderRow
	next              uint64
	historyDelay      time.Duration
	synthetic         []*pb.WashTradeRequest
	historyHold       atomic.Bool
	historyEntered    chan struct{}
	historyRelease    chan struct{}
	lostTerminalReply atomic.Bool
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
	if s.lostTerminalReply.CompareAndSwap(true, false) {
		s.orders[s.next].Status = native.StatusCanceled
		return nil, status.Error(codes.Unavailable, "injected lost reply after terminal native effect")
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
	if r.History && s.historyHold.Load() {
		select {
		case s.historyEntered <- struct{}{}:
		default:
		}
		select {
		case <-s.historyRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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
