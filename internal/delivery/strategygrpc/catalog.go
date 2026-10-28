package strategygrpc

import (
	"context"

	"github.com/shopspring/decimal"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// CatalogServer borrows viewer queries and owns no task or trading resource.
type CatalogServer struct {
	strategyv1.UnimplementedStrategyCatalogServer
	catalog domain.AdminCatalog
}

// NewCatalog constructs the explicit catalog boundary without I/O.
func NewCatalog(catalog domain.AdminCatalog) (*CatalogServer, error) {
	if catalog == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &CatalogServer{catalog: catalog}, nil
}

// ListAccounts keeps disabled accounts explicit and fee ratios exact.
func (s *CatalogServer) ListAccounts(ctx context.Context, request *strategyv1.ListAccountsRequest) (*strategyv1.ListAccountsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.catalog.Accounts(ctx, request.Session.Token, domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.ListAccountsReply{
		Accounts:    make([]*strategyv1.CatalogAccount, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, row := range page.Items {
		if !domain.FixedDecimal(row.QuoteFeeRate, 1, 18, true) || row.QuoteFeeRate.GreaterThanOrEqual(decimal.NewFromInt(1)) {
			return nil, rpcError(domain.ErrAdminUnavailable)
		}
		reply.Accounts = append(reply.Accounts, &strategyv1.CatalogAccount{
			Id:           row.ID,
			AccountId:    row.AccountID,
			SubjectId:    row.SubjectID,
			Enabled:      row.Enabled,
			QuoteFeeRate: row.QuoteFeeRate.String(),
			Revision:     row.Revision,
		})
	}
	return reply, nil
}

// ListInstruments returns execution precision independently of external symbols.
func (s *CatalogServer) ListInstruments(ctx context.Context, request *strategyv1.ListInstrumentsRequest) (*strategyv1.ListInstrumentsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.catalog.Instruments(ctx, request.Session.Token, domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.ListInstrumentsReply{
		Instruments: make([]*strategyv1.CatalogInstrument, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, row := range page.Items {
		if !domain.FixedDecimal(row.Rules.PriceTick, 20, 18, false) || !domain.FixedDecimal(row.Rules.QuantityStep, 20, 18, false) || !domain.FixedDecimal(row.Rules.MinNotional, 20, 18, false) {
			return nil, rpcError(domain.ErrAdminUnavailable)
		}
		reply.Instruments = append(reply.Instruments, &strategyv1.CatalogInstrument{
			Id:              row.Rules.ID,
			ExecutionVenue:  row.ExecutionVenue,
			Symbol:          row.Symbol,
			BaseAsset:       row.BaseAsset,
			QuoteAsset:      row.QuoteAsset,
			PriceTick:       row.Rules.PriceTick.String(),
			QuantityStep:    row.Rules.QuantityStep.String(),
			MinNotional:     row.Rules.MinNotional.String(),
			Status:          row.Rules.Status,
			MetadataVersion: row.Rules.MetadataVersion,
		})
	}
	return reply, nil
}

// ListMarketFeeds requires explicit execution product and LIVE/REPLAY mode.
func (s *CatalogServer) ListMarketFeeds(ctx context.Context, request *strategyv1.ListMarketFeedsRequest) (*strategyv1.ListMarketFeedsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	mode := domain.MarketMode(request.Mode)
	if request.InstrumentId <= 0 || (mode != domain.MarketModeLive && mode != domain.MarketModeReplay) || !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.catalog.Feeds(ctx, request.Session.Token, request.InstrumentId, mode, domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.ListMarketFeedsReply{
		Feeds:       make([]*strategyv1.CatalogMarketFeed, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, row := range page.Items {
		if row.Validate() != nil || row.ID <= 0 || row.InstrumentID != request.InstrumentId || row.Mode != mode {
			return nil, rpcError(domain.ErrAdminUnavailable)
		}
		reply.Feeds = append(reply.Feeds, &strategyv1.CatalogMarketFeed{
			Id:             row.ID,
			Source:         string(row.Source),
			InstrumentId:   row.InstrumentID,
			ExternalSymbol: row.Symbol,
			Mode:           string(row.Mode),
			SourceVersion:  row.SourceVersion,
			Policy: &commonv1.MarketPolicy{
				MaximumAgeMs:     row.Policy.MaximumAge.Milliseconds(),
				MaximumSkewMs:    row.Policy.MaximumSkew.Milliseconds(),
				MaximumSpreadBps: row.Policy.MaximumSpreadBP.String(),
				MaximumJumpBps:   row.Policy.MaximumJumpBP.String(),
				RecoverySamples:  int32(row.Policy.RecoverySamples),
				RecoveryWindowMs: row.Policy.RecoveryWindow.Milliseconds(),
			},
		})
	}
	return reply, nil
}
