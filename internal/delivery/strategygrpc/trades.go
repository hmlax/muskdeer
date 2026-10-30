package strategygrpc

import (
	"context"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// TradeQueryServer borrows snapshot reads without delivering or applying fills.
type TradeQueryServer struct {
	strategyv1.UnimplementedStrategyTradeQueryServer
	trades domain.AdminTrades
}

// NewTradeQuery constructs the current-viewer, verified-API trade boundary.
func NewTradeQuery(trades domain.AdminTrades) (*TradeQueryServer, error) {
	if trades == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &TradeQueryServer{trades: trades}, nil
}

// ListStrategyTrades observes core facts and optional portfolio collection.
func (s *TradeQueryServer) ListStrategyTrades(ctx context.Context, request *strategyv1.ListStrategyTradesRequest) (*strategyv1.ListStrategyTradesReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminTradeFilter{
		StrategyID: request.StrategyId,
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		OrderID: request.OrderId,
		Side:    domain.Side(request.Side),
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.trades.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListStrategyTradesReply{
		Trades:      make([]*strategyv1.StrategyTrade, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, trade := range page.Items {
		value, err := strategywire.TradeProto(trade)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Trades = append(reply.Trades, value)
	}
	if !strategywire.ValidTradePage(reply, filter) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetStrategyTrade requires both the strategy and authoritative core fill ID.
func (s *TradeQueryServer) GetStrategyTrade(ctx context.Context, request *strategyv1.GetStrategyTradeRequest) (*strategyv1.GetStrategyTradeReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 || request.TradeId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	trade, err := s.trades.Detail(ctx, request.Session.Token, request.StrategyId, request.TradeId)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := strategywire.TradeProto(trade)
	if err != nil || value.Id != request.TradeId || value.StrategyId != request.StrategyId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return &strategyv1.GetStrategyTradeReply{Trade: value}, nil
}

var _ strategyv1.StrategyTradeQueryServer = (*TradeQueryServer)(nil)
