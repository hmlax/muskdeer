package strategygrpc

import (
	"context"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// PortfolioQueryServer exposes account read models without portfolio jobs.
type PortfolioQueryServer struct {
	strategyv1.UnimplementedStrategyPortfolioQueryServer
	portfolios domain.AdminPortfolios
}

// NewPortfolioQuery borrows current-viewer reads at the verified API boundary.
func NewPortfolioQuery(portfolios domain.AdminPortfolios) (*PortfolioQueryServer, error) {
	if portfolios == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &PortfolioQueryServer{portfolios: portfolios}, nil
}

// ListPortfolios pages account projections and independent core observations.
func (s *PortfolioQueryServer) ListPortfolios(ctx context.Context, request *strategyv1.ListPortfoliosRequest) (*strategyv1.ListPortfoliosReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminPortfolioFilter{
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		AccountID:    request.AccountId,
		InstrumentID: request.InstrumentId,
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.portfolios.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListPortfoliosReply{
		Portfolios:  make([]*strategyv1.AccountPortfolio, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, portfolio := range page.Items {
		value, err := strategywire.PortfolioProto(portfolio)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Portfolios = append(reply.Portfolios, value)
	}
	if !strategywire.ValidPortfolioPage(reply, filter) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetPortfolio binds the response to the exact persisted account projection ID.
func (s *PortfolioQueryServer) GetPortfolio(ctx context.Context, request *strategyv1.GetPortfolioRequest) (*strategyv1.GetPortfolioReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.PortfolioId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	portfolio, err := s.portfolios.Detail(ctx, request.Session.Token, request.PortfolioId)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := strategywire.PortfolioProto(portfolio)
	if err != nil || value.Id != request.PortfolioId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return &strategyv1.GetPortfolioReply{Portfolio: value}, nil
}

var _ strategyv1.StrategyPortfolioQueryServer = (*PortfolioQueryServer)(nil)
