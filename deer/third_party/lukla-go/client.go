// Package orderclient is a Go gRPC SDK for the order-service (contract trading API).
//
//	order-service listens on gRPC (default :8080). Generate stubs after proto changes:
//	  make -C sdk/go/lukla-go generate
package orderclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "gitlab.com/himalaya/lukla-go/pb"
)

// Re-export commonly used proto enums and messages.
type (
	Side         = pb.Side
	OrderKind    = pb.OrderKind
	TimeInForce  = pb.TimeInForce
	PositionType = pb.PositionType
	ErrorCode    = pb.ErrorCode

	OpenPositionRequest  = pb.OpenPositionRequest
	PlaceRequest         = pb.PlaceRequest
	ClosePositionRequest = pb.ClosePositionRequest
	PlaceOrderResponse   = pb.PlaceOrderResponse
	CancelOrderRequest   = pb.CancelOrderRequest
	CancelOrderResponse  = pb.CancelOrderResponse
	GetAccountResponse   = pb.GetAccountResponse
	OrderRow             = pb.OrderRow
	DealRow              = pb.DealRow
	PositionRow          = pb.PositionRow
)

// Client talks to order-service over gRPC.
type Client struct {
	conn *grpc.ClientConn
	api  pb.OrderClient
	cfg  config
}

// New dials target (e.g. "127.0.0.1:8080" or "dns:///order-service:8080").
func New(ctx context.Context, target string, opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.authToken == "" {
		return nil, fmt.Errorf("orderclient: WithAuthToken or CLIENT_AUTH_TOKEN is required")
	}
	withAuth := func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+cfg.authToken, "x-orderbook-protocol", "2")
	}
	dialOpts := append([]grpc.DialOption{
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(withAuth(ctx), method, req, reply, cc, opts...)
		}),
		grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return streamer(withAuth(ctx), desc, cc, method, opts...)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}, cfg.dialOptions...)

	_ = ctx
	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("orderclient dial %q: %w", target, err)
	}

	return &Client{
		conn: conn,
		api:  pb.NewOrderClient(conn),
		cfg:  cfg,
	}, nil
}

// Close shuts down the gRPC connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Raw exposes the generated client for streaming RPCs (PlaceStream, PingStream, etc.).
func (c *Client) Raw() pb.OrderClient {
	return c.api
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if c.cfg.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.cfg.timeout)
}

func call[T any](c *Client, ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := fn(ctx)
	return out, wrapGRPCError(err)
}

// ── Health ───────────────────────────────────────────────────────────────────

func (c *Client) Ping(ctx context.Context) (*pb.PingResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.PingResponse, error) {
		return c.api.Ping(ctx, &pb.PingRequest{})
	})
}

// ── Account ──────────────────────────────────────────────────────────────────

func (c *Client) GetAccount(ctx context.Context, userID int64) (*pb.GetAccountResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.GetAccountResponse, error) {
		return c.api.GetAccount(ctx, &pb.GetAccountRequest{UserId: userID})
	})
}

func (c *Client) SetPositionMode(ctx context.Context, req *pb.SetPositionModeRequest) (*pb.SetPositionModeResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.SetPositionModeResponse, error) {
		return c.api.SetPositionMode(ctx, req)
	})
}

func (c *Client) Deposit(ctx context.Context, userID int64, coin, amount, requestID string) error {
	_, err := call(c, ctx, func(ctx context.Context) (*pb.Empty, error) {
		return c.api.Deposit(ctx, &pb.AmountRequest{
			UserId:    userID,
			Coin:      coin,
			Amount:    amount,
			RequestId: requestID,
		})
	})
	return err
}

func (c *Client) TransferIn(ctx context.Context, userID int64, coin, amount, requestID string) error {
	_, err := call(c, ctx, func(ctx context.Context) (*pb.Empty, error) {
		return c.api.TransferIn(ctx, &pb.AmountRequest{
			UserId:    userID,
			Coin:      coin,
			Amount:    amount,
			RequestId: requestID,
		})
	})
	return err
}

// TransferOut uses one stable external request ID. Omission returns InvalidArgument.
// The variadic argument preserves compatibility with existing callers at compile time.
func (c *Client) TransferOut(ctx context.Context, userID int64, coin, amount string, requestID ...string) (*pb.TransferOutResponse, error) {
	if len(requestID) != 1 || requestID[0] == "" {
		return nil, status.Error(codes.InvalidArgument, "one stable withdrawal request ID is required")
	}
	return c.TransferOutForFund(ctx, userID, 0, coin, amount, requestID[0])
}

// ── Queries ────────────────────────────────────────────────────────────────────

func (c *Client) ListPositions(ctx context.Context, userID int64) (*pb.ListPositionsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListPositionsResponse, error) {
		return c.api.ListPositions(ctx, &pb.ListPositionsRequest{UserId: userID})
	})
}

func (c *Client) ListOrders(ctx context.Context, userID int64, symbol string) (*pb.ListOrdersResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListOrdersResponse, error) {
		return c.api.ListOrders(ctx, &pb.ListOrdersRequest{UserId: userID, Symbol: symbol})
	})
}

func (c *Client) ListDeals(ctx context.Context, userID int64, symbol string, limit uint32) (*pb.ListDealsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListDealsResponse, error) {
		return c.api.ListDeals(ctx, &pb.ListDealsRequest{UserId: userID, Symbol: symbol, Limit: limit})
	})
}

func (c *Client) ListBills(ctx context.Context, userID int64, coin string) (*pb.ListBillsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListBillsResponse, error) {
		return c.api.ListBills(ctx, &pb.ListBillsRequest{UserId: userID, Coin: coin})
	})
}

// ── Orders ─────────────────────────────────────────────────────────────────────

func (c *Client) OpenPosition(ctx context.Context, req *pb.OpenPositionRequest) (*pb.PlaceOrderResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.PlaceOrderResponse, error) {
		return c.api.OpenPosition(ctx, req)
	})
}

func (c *Client) OpenPositionWith(ctx context.Context, in OpenPositionInput) (*pb.PlaceOrderResponse, error) {
	return c.OpenPosition(ctx, in.Proto())
}

func (c *Client) Place(ctx context.Context, req *pb.PlaceRequest) (*pb.PlaceOrderResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.PlaceOrderResponse, error) {
		return c.api.Place(ctx, req)
	})
}

func (c *Client) PlaceWith(ctx context.Context, in PlaceInput) (*pb.PlaceOrderResponse, error) {
	return c.Place(ctx, in.Proto())
}

func (c *Client) ClosePosition(ctx context.Context, req *pb.ClosePositionRequest) (*pb.PlaceOrderResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.PlaceOrderResponse, error) {
		return c.api.ClosePosition(ctx, req)
	})
}

func (c *Client) ClosePositionWith(ctx context.Context, in ClosePositionInput) (*pb.PlaceOrderResponse, error) {
	return c.ClosePosition(ctx, in.Proto())
}

func (c *Client) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.CancelOrderResponse, error) {
		return c.api.CancelOrder(ctx, req)
	})
}

func (c *Client) CancelOrderWith(ctx context.Context, in CancelInput) (*pb.CancelOrderResponse, error) {
	return c.CancelOrder(ctx, in.Proto())
}

// ── Market data helper ─────────────────────────────────────────────────────────

func (c *Client) WashTrade(ctx context.Context, req *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.WashTradeResponse, error) {
		return c.api.WashTrade(ctx, req)
	})
}

// GetAccountForFund operates on the explicitly selected fund.
func (c *Client) GetAccountForFund(ctx context.Context, userID, fundID int64) (*pb.GetAccountResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.GetAccountResponse, error) {
		return c.api.GetAccount(ctx, &pb.GetAccountRequest{
			UserId: userID,
			FundId: fundID,
		})
	})
}

// DepositForFund operates on the explicitly selected fund.
func (c *Client) DepositForFund(ctx context.Context, userID, fundID int64, coin, amount, requestID string) error {
	if requestID == "" {
		return status.Error(codes.InvalidArgument, "stable transfer request ID is required")
	}
	_, err := call(c, ctx, func(ctx context.Context) (*pb.Empty, error) {
		return c.api.Deposit(ctx, &pb.AmountRequest{
			UserId:    userID,
			FundId:    fundID,
			Coin:      coin,
			Amount:    amount,
			RequestId: requestID,
		})
	})
	return err
}

// TransferInForFund operates on the explicitly selected fund.
func (c *Client) TransferInForFund(ctx context.Context, userID, fundID int64, coin, amount, requestID string) error {
	if requestID == "" {
		return status.Error(codes.InvalidArgument, "stable transfer request ID is required")
	}
	_, err := call(c, ctx, func(ctx context.Context) (*pb.Empty, error) {
		return c.api.TransferIn(ctx, &pb.AmountRequest{
			UserId:    userID,
			FundId:    fundID,
			Coin:      coin,
			Amount:    amount,
			RequestId: requestID,
		})
	})
	return err
}

// TransferOutForFund operates on the explicitly selected fund.
func (c *Client) TransferOutForFund(ctx context.Context, userID, fundID int64, coin, amount, requestID string) (*pb.TransferOutResponse, error) {
	if requestID == "" {
		return nil, status.Error(codes.InvalidArgument, "stable transfer request ID is required")
	}
	return call(c, ctx, func(ctx context.Context) (*pb.TransferOutResponse, error) {
		return c.api.TransferOut(ctx, &pb.AmountRequest{
			UserId:    userID,
			FundId:    fundID,
			Coin:      coin,
			Amount:    amount,
			RequestId: requestID,
		})
	})
}

// ListPositionsForFund operates on the explicitly selected fund.
func (c *Client) ListPositionsForFund(ctx context.Context, userID, fundID int64) (*pb.ListPositionsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListPositionsResponse, error) {
		return c.api.ListPositions(ctx, &pb.ListPositionsRequest{
			UserId: userID,
			FundId: fundID,
		})
	})
}

// ListOrdersForFund operates on the explicitly selected fund.
func (c *Client) ListOrdersForFund(ctx context.Context, userID, fundID int64, symbol string) (*pb.ListOrdersResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListOrdersResponse, error) {
		return c.api.ListOrders(ctx, &pb.ListOrdersRequest{
			UserId: userID,
			FundId: fundID,
			Symbol: symbol,
		})
	})
}

// ListDealsForFund operates on the explicitly selected fund.
func (c *Client) ListDealsForFund(ctx context.Context, userID, fundID int64, symbol string, limit uint32) (*pb.ListDealsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListDealsResponse, error) {
		return c.api.ListDeals(ctx, &pb.ListDealsRequest{
			UserId: userID,
			FundId: fundID,
			Symbol: symbol,
			Limit:  limit,
		})
	})
}

// ListBillsForFund operates on the explicitly selected fund.
func (c *Client) ListBillsForFund(ctx context.Context, userID, fundID int64, coin string) (*pb.ListBillsResponse, error) {
	return call(c, ctx, func(ctx context.Context) (*pb.ListBillsResponse, error) {
		return c.api.ListBills(ctx, &pb.ListBillsRequest{
			UserId: userID,
			FundId: fundID,
			Coin:   coin,
		})
	})
}
