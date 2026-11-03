package orderclient_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"

	orderclient "gitlab.com/himalaya/lukla-go"
	"gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testUserToken = "regression-user-token"
const testAdminToken = "regression-admin-token"

type fundRequest interface {
	GetFundId() int64
	GetUserId() int64
}

type fundServer struct {
	pb.UnimplementedOrderServer
	mu     sync.Mutex
	seen   []fundRequest
	debits map[string]struct{}
}

func (s *fundServer) record(req fundRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, req)
}
func (s *fundServer) GetAccount(_ context.Context, req *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	s.record(req)
	return &pb.GetAccountResponse{
		UserId: req.UserId,
		FundId: req.FundId,
	}, nil
}
func (s *fundServer) Deposit(_ context.Context, req *pb.AmountRequest) (*pb.Empty, error) {
	s.record(req)
	return &pb.Empty{}, nil
}
func (s *fundServer) TransferIn(_ context.Context, req *pb.AmountRequest) (*pb.Empty, error) {
	s.record(req)
	return &pb.Empty{}, nil
}
func (s *fundServer) TransferOut(_ context.Context, req *pb.AmountRequest) (*pb.TransferOutResponse, error) {
	s.record(req)
	if req.RequestId == "" {
		return nil, status.Error(codes.InvalidArgument, "missing id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debits[req.RequestId] = struct{}{}
	return &pb.TransferOutResponse{Ok: true}, nil
}
func (s *fundServer) TransferOutStream(stream grpc.BidiStreamingServer[pb.AmountRequest, pb.TransferOutResponse]) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		result, err := s.TransferOut(stream.Context(), req)
		if err != nil {
			return err
		}
		if err := stream.Send(result); err != nil {
			return err
		}
	}
}
func (s *fundServer) ListPositions(_ context.Context, req *pb.ListPositionsRequest) (*pb.ListPositionsResponse, error) {
	s.record(req)
	return &pb.ListPositionsResponse{}, nil
}
func (s *fundServer) ListOrders(_ context.Context, req *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	s.record(req)
	return &pb.ListOrdersResponse{}, nil
}
func (s *fundServer) ListDeals(_ context.Context, req *pb.ListDealsRequest) (*pb.ListDealsResponse, error) {
	s.record(req)
	return &pb.ListDealsResponse{}, nil
}
func (s *fundServer) ListBills(_ context.Context, req *pb.ListBillsRequest) (*pb.ListBillsResponse, error) {
	s.record(req)
	return &pb.ListBillsResponse{}, nil
}

func TestFundMethodsAndAuthenticatedRawTransferRetries(t *testing.T) {
	t.Parallel()
	listener := bufconn.Listen(1024 * 1024)
	authorize := func(ctx context.Context) error {
		md, _ := metadata.FromIncomingContext(ctx)
		tokens := md.Get("authorization")
		protocol := md.Get("x-orderbook-protocol")
		if len(tokens) != 1 || (tokens[0] != "Bearer "+testUserToken && tokens[0] != "Bearer "+testAdminToken) || len(protocol) != 1 || protocol[0] != "2" {
			return status.Error(codes.Unauthenticated, "missing client authentication")
		}
		return nil
	}
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if err := authorize(ctx); err != nil {
				return nil, err
			}
			if info.FullMethod == pb.Order_Deposit_FullMethodName || info.FullMethod == pb.Order_TransferIn_FullMethodName {
				md, _ := metadata.FromIncomingContext(ctx)
				if md.Get("authorization")[0] != "Bearer "+testAdminToken {
					return nil, status.Error(codes.PermissionDenied, "administrator required")
				}
			}
			return handler(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if err := authorize(stream.Context()); err != nil {
				return err
			}
			return handler(srv, stream)
		}),
	)
	backend := &fundServer{debits: make(map[string]struct{})}
	pb.RegisterOrderServer(server, backend)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	connect := func(token string) *orderclient.Client {
		t.Helper()
		client, err := orderclient.New(context.Background(), "passthrough:///fund-test", orderclient.WithAuthToken(token), orderclient.WithDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() })))
		if err != nil {
			t.Fatalf("New(bufconn) = %v, want nil", err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	user := connect(testUserToken)
	admin := connect(testAdminToken)
	ctx := context.Background()
	if _, err := user.TransferOut(ctx, 7, "BTC", "200"); status.Code(err) != codes.InvalidArgument {
		t.Errorf("TransferOut(missing request ID) = %v, want InvalidArgument", err)
	}
	if err := user.DepositForFund(ctx, 7, 9001, "BTC", "1000", "credit-1"); status.Code(err) != codes.PermissionDenied {
		t.Errorf("DepositForFund(user) = %v, want PermissionDenied", err)
	}
	calls := []struct {
		name string
		call func() error
	}{
		{
			name: "GetAccountForFund",
			call: func() error { _, err := user.GetAccountForFund(ctx, 7, 9001); return err },
		},
		{
			name: "DepositForFund",
			call: func() error { return admin.DepositForFund(ctx, 7, 9001, "BTC", "1000", "credit-1") },
		},
		{
			name: "TransferInForFund",
			call: func() error { return admin.TransferInForFund(ctx, 7, 9001, "BTC", "1000", "credit-2") },
		},
		{
			name: "TransferOutForFund",
			call: func() error {
				_, err := user.TransferOutForFund(ctx, 7, 9001, "BTC", "200", "withdrawal-1")
				return err
			},
		},
		{
			name: "TransferOutForFund retry",
			call: func() error {
				_, err := user.TransferOutForFund(ctx, 7, 9001, "BTC", "200", "withdrawal-1")
				return err
			},
		},
		{
			name: "ListPositionsForFund",
			call: func() error { _, err := user.ListPositionsForFund(ctx, 7, 9001); return err },
		},
		{
			name: "ListOrdersForFund",
			call: func() error { _, err := user.ListOrdersForFund(ctx, 7, 9001, "BTCUSDT"); return err },
		},
		{
			name: "ListDealsForFund",
			call: func() error { _, err := user.ListDealsForFund(ctx, 7, 9001, "BTCUSDT", 10); return err },
		},
		{
			name: "ListBillsForFund",
			call: func() error { _, err := user.ListBillsForFund(ctx, 7, 9001, "BTC"); return err },
		},
	}
	for _, call := range calls {
		if err := call.call(); err != nil {
			t.Errorf("%s(fund 9001) = %v, want nil", call.name, err)
		}
	}
	stream, err := user.Raw().TransferOutStream(ctx)
	if err != nil {
		t.Fatalf("Raw.TransferOutStream() = %v, want nil", err)
	}
	if err := stream.Send(&pb.AmountRequest{
		UserId:    7,
		FundId:    9001,
		Coin:      "BTC",
		Amount:    "200",
		RequestId: "withdrawal-1",
	}); err != nil {
		t.Fatalf("Raw.TransferOutStream.Send() = %v, want nil", err)
	}
	if result, err := stream.Recv(); err != nil || !result.GetOk() {
		t.Fatalf("Raw.TransferOutStream.Recv() = %v, %v, want ok", result, err)
	}
	_ = stream.CloseSend()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for _, req := range backend.seen {
		if req.GetFundId() != 9001 || req.GetUserId() != 7 {
			t.Errorf("ForFund request = user %d fund %d, want user 7 fund 9001", req.GetUserId(), req.GetFundId())
		}
		if amount, ok := req.(*pb.AmountRequest); ok && amount.GetCoin() != "BTC" {
			t.Errorf("transfer coin = %q, want BTC", amount.GetCoin())
		}
	}
	if len(backend.debits) != 1 {
		t.Errorf("retry request IDs = %d, want 1", len(backend.debits))
	}
}
