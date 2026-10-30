package strategygrpc

import (
	"context"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// MarketQueryServer borrows reads without opening feeds or granting authority.
type MarketQueryServer struct {
	strategyv1.UnimplementedStrategyMarketQueryServer
	markets domain.AdminMarkets
}

// NewMarketQuery constructs a verified-API/current-viewer query boundary.
func NewMarketQuery(markets domain.AdminMarkets) (*MarketQueryServer, error) {
	if markets == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &MarketQueryServer{markets: markets}, nil
}

// ListMarkets keeps each source/product/mode observation independent.
func (s *MarketQueryServer) ListMarkets(ctx context.Context, request *strategyv1.ListMarketsRequest) (*strategyv1.ListMarketsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminMarketFilter{
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		Source:       domain.MarketSource(request.Source),
		InstrumentID: request.InstrumentId,
		Mode:         domain.MarketMode(request.Mode),
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.markets.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListMarketsReply{
		Markets:     make([]*strategyv1.MarketMonitor, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, market := range page.Items {
		value, err := strategywire.MarketProto(market)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Markets = append(reply.Markets, value)
	}
	if !strategywire.ValidMarketPage(reply, filter) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetMarket binds a current observation to its explicitly requested feed.
func (s *MarketQueryServer) GetMarket(ctx context.Context, request *strategyv1.GetMarketRequest) (*strategyv1.GetMarketReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.FeedId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	market, err := s.markets.Detail(ctx, request.Session.Token, request.FeedId)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := strategywire.MarketProto(market)
	if err != nil || value.Feed.Id != request.FeedId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return &strategyv1.GetMarketReply{Market: value}, nil
}

var _ strategyv1.StrategyMarketQueryServer = (*MarketQueryServer)(nil)
