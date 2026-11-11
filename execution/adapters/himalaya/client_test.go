package himalaya

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	gateway "github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestSDKSubmissionNeedsTheFinalMuskdeerAdmission(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, nil)
	if _, err := client.Submit(t.Context(), sdkOrder()); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("missing admission accepted: %v", err)
	}
	if peer.count() != 0 {
		t.Fatal("an order escaped without a final Muskdeer admission")
	}
}

func TestSDKReadOnlyClientCannotPublishSyntheticMarketData(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, nil)
	if _, err := client.PublishSynthetic(t.Context(), &gateway.SyntheticRequest{}); !errors.Is(err, ErrInvalidSubmit) || peer.count() != 0 {
		t.Fatalf("read-only synthetic publication = %v, SDK effects=%d, want rejected/0", err, peer.count())
	}
}

func TestSDKReplyLossRetainsIdentityWithoutSendingAgain(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{fail: true}
	client := sdkClient(t, peer, &testAdmission{})
	request := sdkOrder()
	if _, err := client.Submit(t.Context(), request); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("reply loss did not become UNKNOWN: %v", err)
	}
	if peer.count() != 1 || peer.received.ClientId != request.ClientID || peer.received.Price != request.Price.String() || peer.received.Quantity != request.Quantity.String() {
		t.Fatal("SDK call repeated or lost the exact correlation/decimal input")
	}
}

func TestSDKGuardCannotAccidentallyInvokeTheWriteTwice(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, &testAdmission{repeat: true})
	if _, err := client.Submit(t.Context(), sdkOrder()); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("ambiguous admission result was hidden: %v", err)
	}
	if peer.count() != 1 {
		t.Fatalf("write repeated %d times", peer.count())
	}
}

func TestSDKWireUsesExistingPlaceAndProtocolAuthentication(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	gate := &testAdmission{}
	client := sdkClient(t, peer, gate)
	request := sdkOrder()
	response, err := client.Submit(t.Context(), request)
	if err != nil || response.GetOrderID() != 42 {
		t.Fatalf("existing SDK Place failed: %v %v", response, err)
	}
	if peer.received.OrderKind != pb.OrderKind_ORDER_KIND_POST_ONLY || peer.received.PositionType != pb.PositionType_POSITION_TYPE_CROSS || peer.received.FundId != request.FundID || peer.protocol != "2" || peer.token != "Bearer sdk-test-token" {
		t.Fatal("existing SDK protocol, fund, post-only or auth contract changed")
	}
	if gate.seen.ClientID != request.ClientID || gate.seen.Digest == [32]byte{} || gate.seen.Epoch != request.Epoch {
		t.Fatal("final admission did not receive the persisted identity and immutable request digest")
	}
}

func TestSDKCurrentIntentWithoutProcessLeaseStillUsesDurableAdmission(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	gate := &testAdmission{}
	client := sdkClient(t, peer, gate)
	request := sdkOrder()
	request.OwnerID, request.Epoch = "", 0
	response, err := client.Submit(t.Context(), request)
	if err != nil || response.GetOrderID() != 42 || peer.count() != 1 {
		t.Fatalf("current durable SDK request = %v, %v; sends=%d", response, err, peer.count())
	}
	if gate.seen.IntentID != request.IntentID || gate.seen.OwnerID != "" || gate.seen.Epoch != 0 || gate.seen.Digest == [32]byte{} {
		t.Fatal("current request bypassed durable admission or invented process authority")
	}
}

func TestSDKInvalidAmountNeverReachesAdmissionOrExchange(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	gate := &testAdmission{}
	client := sdkClient(t, peer, gate)
	request := sdkOrder()
	request.Quantity = decimal.Zero
	if _, err := client.Submit(t.Context(), request); !errors.Is(err, ErrInvalidSubmit) {
		t.Fatalf("invalid quantity accepted: %v", err)
	}
	if peer.count() != 0 || gate.seen.IntentID != 0 {
		t.Fatal("invalid amount reached the persisted admission or SDK")
	}
}

func TestSDKReadRejectsCrossFundPositions(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{wrongFund: true}
	client := sdkClient(t, peer, nil)
	if _, err := client.Read(t.Context(), Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("foreign fund entered snapshot: %v", err)
	}
	if peer.count() != 0 {
		t.Fatal("read-only query submitted an order")
	}
}

type testAdmission struct {
	repeat bool
	seen   Admission
}

func (g *testAdmission) WithSubmission(ctx context.Context, admission Admission, send func(context.Context) error) error {
	g.seen = admission
	if err := send(ctx); err != nil {
		return err
	}
	if g.repeat {
		return send(ctx)
	}
	return nil
}

type sdkPeer struct {
	pb.UnimplementedOrderServer
	mu                sync.Mutex
	calls             int
	received          *pb.PlaceRequest
	gridOpen          *pb.OpenPositionRequest
	gridClose         *pb.ClosePositionRequest
	rejectPlace       bool
	fail              bool
	protocol          string
	traceparent       string
	token             string
	wrongFund         bool
	historyOrder      *pb.OrderRow
	historyOrders     []*pb.OrderRow
	historyRequest    *pb.ListOrdersRequest
	cancelCalls       int
	openOrders        []*pb.OrderRow
	syntheticRequests []*pb.WashTradeRequest
}

func (p *sdkPeer) Place(ctx context.Context, request *pb.PlaceRequest) (*pb.PlaceOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.received = proto.CloneOf(request)
	if p.rejectPlace {
		return nil, status.Error(codes.FailedPrecondition, "Place is only available in ONE_WAY account position mode")
	}
	return p.placeReply(ctx, request.ClientId)
}

func (p *sdkPeer) OpenPosition(ctx context.Context, request *pb.OpenPositionRequest) (*pb.PlaceOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.gridOpen = proto.CloneOf(request)
	return p.placeReply(ctx, request.ClientId)
}

func (p *sdkPeer) ClosePosition(ctx context.Context, request *pb.ClosePositionRequest) (*pb.PlaceOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.gridClose = proto.CloneOf(request)
	return p.placeReply(ctx, request.ClientId)
}

func (p *sdkPeer) placeReply(ctx context.Context, clientID string) (*pb.PlaceOrderResponse, error) {
	headers, _ := metadata.FromIncomingContext(ctx)
	versions, tokens := headers.Get("x-orderbook-protocol"), headers.Get("authorization")
	if len(versions) != 1 || len(tokens) != 1 {
		return nil, status.Error(codes.Unauthenticated, "missing SDK protocol or authentication")
	}
	if values := headers.Get("traceparent"); len(values) == 1 {
		p.traceparent = values[0]
	}
	p.protocol = versions[0]
	p.token = tokens[0]
	if p.fail {
		return nil, status.Error(codes.Unavailable, "injected reply loss after admission")
	}
	return &pb.PlaceOrderResponse{
		OrderId:  42,
		ClientId: clientID,
	}, nil
}

func (*sdkPeer) GetMarket(_ context.Context, request *pb.GetMarketRequest) (*pb.GetMarketResponse, error) {
	return &pb.GetMarketResponse{Market: &pb.MarketView{
		Symbol:       request.Symbol,
		MakerFeeRate: "0.0002",
		TakerFeeRate: "0.0005",
	}}, nil
}

func (*sdkPeer) GetAccount(_ context.Context, request *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	return &pb.GetAccountResponse{
		UserId: request.UserId,
		FundId: request.FundId,
	}, nil
}

func (p *sdkPeer) ListPositions(_ context.Context, request *pb.ListPositionsRequest) (*pb.ListPositionsResponse, error) {
	fund := request.FundId
	if p.wrongFund {
		fund++
	}
	return &pb.ListPositionsResponse{Positions: []*pb.PositionRow{
		{
			UserId: request.UserId,
			FundId: fund,
		},
	}}, nil
}

func (p *sdkPeer) ListOrders(_ context.Context, request *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.historyRequest = proto.CloneOf(request)
	if !request.History && len(p.openOrders) != 0 {
		return &pb.ListOrdersResponse{Orders: p.openOrders}, nil
	}
	if request.History && len(p.historyOrders) != 0 {
		orders := make([]*pb.OrderRow, 0, len(p.historyOrders))
		for _, order := range p.historyOrders {
			orders = append(orders, proto.CloneOf(order))
		}
		return &pb.ListOrdersResponse{Orders: orders}, nil
	}
	if request.History && p.historyOrder != nil {
		return &pb.ListOrdersResponse{Orders: []*pb.OrderRow{proto.CloneOf(p.historyOrder)}}, nil
	}
	return &pb.ListOrdersResponse{}, nil
}

func (p *sdkPeer) CancelOrder(context.Context, *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelCalls++
	return &pb.CancelOrderResponse{Success: true}, nil
}

func (*testAdmission) WithCancellation(ctx context.Context, _ CancelAdmission, send func(context.Context) error) error {
	return send(ctx)
}

func (p *sdkPeer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func sdkClient(t *testing.T, peer *sdkPeer, admission SubmissionGuard) *Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterOrderServer(server, peer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := NewClient(t.Context(), Config{
		Target:  "passthrough:///provided-sdk",
		Token:   "sdk-test-token",
		Timeout: time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		},
	}, admission)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return client
}

func sdkOrder() SubmitRequest {
	return SubmitRequest{
		IntentID: 1,
		OwnerID:  "execution-test",
		Epoch:    7,
		UserID:   10001,
		FundID:   10001,
		Symbol:   "BTCUSDT",
		ClientID: "deer-sdk-test-1",
		Side:     domain.SideBuy,
		Price:    decimal.RequireFromString("100.100000000000000001"),
		Quantity: decimal.RequireFromString("0.001"),
		Leverage: 10,
	}
}

func (p *sdkPeer) WashTrade(_ context.Context, request *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syntheticRequests = append(p.syntheticRequests, proto.CloneOf(request))
	return &pb.WashTradeResponse{
		TradeId:     "adapter-tape-test",
		Price:       request.Price,
		Quantity:    request.Quantity,
		TimestampMs: 1,
	}, nil
}
