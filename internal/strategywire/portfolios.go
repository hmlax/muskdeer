package strategywire

import (
	"github.com/shopspring/decimal"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// PortfolioProto keeps stored diagnostics and independent current observations.
func PortfolioProto(value domain.AdminPortfolio) (*strategyv1.AccountPortfolio, error) {
	p := value.Projection
	if !portfolioDomainAmounts(value) || p.Opening.AccountID != p.AccountID || p.Opening.InstrumentID != p.InstrumentID {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.AccountPortfolio{
		Id:              value.ID,
		ExecutionVenue:  value.ExecutionVenue,
		Symbol:          value.Symbol,
		AccountId:       p.AccountID,
		InstrumentId:    p.InstrumentID,
		BaseAsset:       p.BaseAsset,
		QuoteAsset:      p.QuoteAsset,
		AccountEnabled:  value.AccountEnabled,
		AccountRevision: value.AccountRevision,
		Projection: &strategyv1.StoredPortfolioProjection{
			OpeningQuantity:    p.Opening.Quantity.String(),
			OpeningCashQuote:   p.Opening.CashQuote.String(),
			OpeningCostQuote:   p.Opening.CostQuote.String(),
			Quantity:           p.Values.Quantity.String(),
			CashQuote:          p.Values.CashQuote.String(),
			CostQuote:          p.Values.CostQuote.String(),
			RealizedPnlQuote:   p.Values.RealizedPnlQuote.String(),
			FeesQuote:          p.Values.FeesQuote.String(),
			LastSourceSequence: p.LastSourceSequence,
			StoredReconciled:   p.Reconciled,
			Revision:           p.Revision,
			ErrorCode:          p.ErrorCode,
		},
		Source: &strategyv1.PortfolioFactProgress{
			Count:                value.Source.Count,
			LatestSourceSequence: value.Source.LatestSourceSequence,
		},
		Collected: &strategyv1.PortfolioFactProgress{
			Count:                value.Collected.Count,
			LatestSourceSequence: value.Collected.LatestSourceSequence,
		},
		CoreBaseBalance:  portfolioBalanceProto(value.CoreBaseBalance),
		CoreQuoteBalance: portfolioBalanceProto(value.CoreQuoteBalance),
		CreatedAtMs:      value.CreatedAt.UnixMilli(),
		UpdatedAtMs:      value.UpdatedAt.UnixMilli(),
	}
	if !ValidPortfolio(reply) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func portfolioDomainAmounts(value domain.AdminPortfolio) bool {
	p := value.Projection
	for _, amount := range []decimal.Decimal{p.Opening.Quantity, p.Opening.CashQuote, p.Opening.CostQuote, p.Values.Quantity, p.Values.CashQuote, p.Values.CostQuote, p.Values.FeesQuote, p.Values.RealizedPnlQuote.Abs()} {
		if !domain.FixedDecimal(amount, 40, 36, true) {
			return false
		}
	}
	for _, balance := range []*domain.AdminPortfolioBalance{value.CoreBaseBalance, value.CoreQuoteBalance} {
		if balance != nil && (!domain.FixedDecimal(balance.Available, 40, 36, true) || !domain.FixedDecimal(balance.Frozen, 40, 36, true)) {
			return false
		}
	}
	return true
}

func portfolioBalanceProto(value *domain.AdminPortfolioBalance) *strategyv1.PortfolioCoreBalance {
	if value == nil {
		return nil
	}
	return &strategyv1.PortfolioCoreBalance{
		Id:          value.ID,
		Asset:       value.Asset,
		Available:   value.Available.String(),
		Frozen:      value.Frozen.String(),
		Revision:    value.Revision,
		UpdatedAtMs: value.UpdatedAt.UnixMilli(),
	}
}

// ValidPortfolioPage binds all account observations to the original filters.
func ValidPortfolioPage(value *strategyv1.ListPortfoliosReply, filter domain.AdminPortfolioFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || len(value.Portfolios) > filter.Page.Limit || value.NextAfterId < 0 {
		return false
	}
	previous := filter.Page.AfterID
	for _, portfolio := range value.Portfolios {
		if !ValidPortfolio(portfolio) || portfolio.Id <= previous ||
			filter.AccountID != "" && portfolio.AccountId != filter.AccountID || filter.InstrumentID != 0 && portfolio.InstrumentId != filter.InstrumentID {
			return false
		}
		previous = portfolio.Id
	}
	return value.NextAfterId == 0 || len(value.Portfolios) == filter.Page.Limit && value.NextAfterId == previous
}

// ValidPortfolio deliberately does not equate stored reconciliation, source
// counts and current funds, or order independent node recording clocks.
func ValidPortfolio(value *strategyv1.AccountPortfolio) bool {
	if !validPortfolioIdentity(value) || !validStoredPortfolio(value.Projection) || !validPortfolioProgress(value.Source) || !validPortfolioProgress(value.Collected) {
		return false
	}
	return validPortfolioBalance(value.CoreBaseBalance, value.BaseAsset) && validPortfolioBalance(value.CoreQuoteBalance, value.QuoteAsset)
}

func validPortfolioIdentity(value *strategyv1.AccountPortfolio) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && value.Id > 0 && value.InstrumentId > 0 && value.AccountRevision > 0 &&
		value.ExecutionVenue == domain.ExecutionVenueSimulator && riskText(value.Symbol, 64) && riskText(value.AccountId, 64) &&
		riskText(value.BaseAsset, 32) && riskText(value.QuoteAsset, 32) && value.BaseAsset != value.QuoteAsset && value.CreatedAtMs > 0 && value.UpdatedAtMs > 0
}

func validStoredPortfolio(value *strategyv1.StoredPortfolioProjection) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || value.Revision <= 0 || value.LastSourceSequence < 0 ||
		value.ErrorCode != "" && value.ErrorCode != domain.PortfolioErrorIncomplete && value.ErrorCode != domain.PortfolioErrorMismatch {
		return false
	}
	for _, amount := range []string{value.Quantity, value.CashQuote, value.CostQuote, value.OpeningCashQuote, value.FeesQuote} {
		if _, err := domain.ParseLedgerDecimal(amount, false, true); err != nil {
			return false
		}
	}
	quantity, quantityErr := domain.ParseLedgerDecimal(value.OpeningQuantity, false, true)
	cost, costErr := domain.ParseLedgerDecimal(value.OpeningCostQuote, false, true)
	_, pnlErr := domain.ParseLedgerDecimal(value.RealizedPnlQuote, true, true)
	return quantityErr == nil && costErr == nil && pnlErr == nil && (!quantity.IsZero() || cost.IsZero())
}

func validPortfolioProgress(value *strategyv1.PortfolioFactProgress) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && value.Count >= 0 && value.LatestSourceSequence >= 0 &&
		(value.Count == 0) == (value.LatestSourceSequence == 0)
}

func validPortfolioBalance(value *strategyv1.PortfolioCoreBalance, asset string) bool {
	if value == nil {
		return true
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 || value.Id <= 0 || value.Asset != asset || value.Revision <= 0 || value.UpdatedAtMs <= 0 {
		return false
	}
	_, availableErr := domain.ParseLedgerDecimal(value.Available, false, true)
	_, frozenErr := domain.ParseLedgerDecimal(value.Frozen, false, true)
	return availableErr == nil && frozenErr == nil
}
