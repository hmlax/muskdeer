package console

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func (s *Server) account(ctx context.Context, actor *pb.Actor, id string) (*commonmodel.TradingAccountPolicy, error) {
	if err := accountScope(actor); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	var row commonmodel.TradingAccountPolicy
	if err := scoped(fresh(ctx, s.db).Model(&commonmodel.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", id), actor).Take(&row).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	return &row, nil
}

func (s *Server) execution(ctx context.Context, account string) (execution.ExecutionClient, error) {
	owner, err := repository.NativeExecutionInstance(ctx, s.db, account)
	if err != nil {
		return nil, s.fail(ctx, err)
	}
	conn, err := s.factory.Client(rpcclient.Target{
		Service:    rpcsecurity.ServiceExecution,
		InstanceID: owner,
	})
	if err != nil {
		return nil, s.fail(ctx, err)
	}
	return execution.NewExecutionClient(conn), nil
}

// GetAccountSnapshot reads current orders and positions from the matching gateway.
func (s *Server) GetAccountSnapshot(ctx context.Context, in *pb.AccountRequest) (*pb.AccountSnapshot, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	account, err := s.account(ctx, in.GetActor(), in.GetAccountId())
	if err != nil {
		return nil, err
	}
	client, err := s.execution(ctx, account.AccountId)
	if err != nil {
		return nil, err
	}
	snapshot, err := client.GetSnapshot(ctx, &common.VenueScope{
		UserId: account.UserId,
		FundId: account.FundId,
		Symbol: account.Symbol,
	})
	if err != nil {
		return nil, s.fail(ctx, err)
	}
	out := &pb.AccountSnapshot{
		AccountId:    account.AccountId,
		Symbol:       account.Symbol,
		ValidUntilMs: snapshot.ValidUntilMs,
		Available:    snapshot.Available,
		MarkPrice:    snapshot.GetMarket().GetMarkPrice(),
		ObservedAtMs: snapshot.ObservedAtMs,
	}
	for _, order := range snapshot.Orders {
		out.Orders = append(out.Orders, &pb.ActiveOrder{
			OrderId:          order.OrderId,
			ClientId:         order.ClientId,
			Side:             order.Side,
			Price:            order.Price,
			Quantity:         order.Quantity,
			ExecutedQuantity: order.ExecutedQuantity,
			Status:           order.Status,
		})
	}
	for _, position := range snapshot.Positions {
		out.Positions = append(out.Positions, &pb.Position{
			Symbol:         position.Symbol,
			SignedQuantity: position.SignedQuantity,
			EntryPrice:     position.EntryPrice,
			Leverage:       position.Leverage,
		})
	}
	return out, nil
}

// CancelInstanceOrders requests managed-order cancellation for a stopped instance.
func (s *Server) CancelInstanceOrders(ctx context.Context, in *pb.CancelRequest) (*pb.CancelReply, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	if !in.GetCancelAll() {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	row, err := s.row(ctx, in.Actor, in.InstanceId)
	if err != nil {
		return nil, err
	}
	if row.Revision != in.ExpectedRevision || row.DesiredRunning {
		return nil, status.Error(codes.FailedPrecondition, "stop the current instance revision before cancellation")
	}
	client, err := s.execution(ctx, row.AccountId)
	if err != nil {
		return nil, err
	}
	reply, err := client.CancelInstanceOrders(ctx, &execution.CancelInstanceRequest{
		Id:        row.Id,
		Revision:  row.Revision,
		CancelAll: true,
	})
	if err != nil {
		return nil, s.fail(ctx, err)
	}
	return &pb.CancelReply{
		Acknowledged:        reply.Cancelled,
		ConfirmationPending: true,
	}, nil
}
