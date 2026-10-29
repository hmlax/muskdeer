package strategygrpc

import (
	"context"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// OrderQueryServer borrows snapshot reads without owning trading work.
type OrderQueryServer struct {
	strategyv1.UnimplementedStrategyOrderQueryServer
	orders domain.AdminOrders
}

// NewOrderQuery constructs the current-viewer, verified-API order boundary.
func NewOrderQuery(orders domain.AdminOrders) (*OrderQueryServer, error) {
	if orders == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &OrderQueryServer{orders: orders}, nil
}

// ListStrategyOrders pages one strategy's OMS facts and newer core observations.
func (s *OrderQueryServer) ListStrategyOrders(ctx context.Context, request *strategyv1.ListStrategyOrdersRequest) (*strategyv1.ListStrategyOrdersReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminOrderFilter{
		StrategyID: request.StrategyId,
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		State:    domain.OrderState(request.State),
		OpenOnly: request.OpenOnly,
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.orders.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListStrategyOrdersReply{
		Orders:      make([]*strategyv1.StrategyOrder, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, order := range page.Items {
		value, err := strategywire.OrderProto(order)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Orders = append(reply.Orders, value)
	}
	if !strategywire.ValidOrderPage(reply, filter) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetStrategyOrder requires both strategy and OMS identity; reads have no effects.
func (s *OrderQueryServer) GetStrategyOrder(ctx context.Context, request *strategyv1.GetStrategyOrderRequest) (*strategyv1.GetStrategyOrderReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 || request.OrderId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	order, err := s.orders.Detail(ctx, request.Session.Token, request.StrategyId, request.OrderId)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := strategywire.OrderProto(order)
	if err != nil || value.Id != request.OrderId || value.StrategyId != request.StrategyId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return &strategyv1.GetStrategyOrderReply{Order: value}, nil
}

var _ strategyv1.StrategyOrderQueryServer = (*OrderQueryServer)(nil)
