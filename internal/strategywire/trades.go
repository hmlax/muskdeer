package strategywire

import (
	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// TradeProto exposes an authoritative fill and independent collection presence.
func TradeProto(value domain.AdminTrade) (*strategyv1.StrategyTrade, error) {
	if !domain.FixedDecimal(value.Price, 20, 18, false) || !domain.FixedDecimal(value.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(value.QuoteNotional, 40, 36, false) || !domain.FixedDecimal(value.FeeAmount, 40, 36, true) {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.StrategyTrade{
		Id:               value.ID,
		ExecutionVenue:   value.ExecutionVenue,
		TradeId:          value.TradeID,
		StrategyId:       value.StrategyID,
		AccountId:        value.AccountID,
		InstrumentId:     value.InstrumentID,
		ConfigVersion:    value.ConfigVersion,
		MarketDataSource: string(value.Source),
		CoreOrderId:      value.CoreOrderID,
		ClientOrderId:    value.ClientOrderID,
		Side:             string(value.Side),
		Price:            value.Price.String(),
		Quantity:         value.Quantity.String(),
		QuoteNotional:    value.QuoteNotional.String(),
		FeeAmount:        value.FeeAmount.String(),
		FeeAsset:         value.FeeAsset,
		RecordedAtMs:     value.RecordedAt.UnixMilli(),
	}
	if value.OrderID != nil {
		id := *value.OrderID
		reply.OrderId = &id
	}
	if fact := value.PortfolioFact; fact != nil {
		reply.PortfolioFact = &strategyv1.TradePortfolioFact{
			Id:           fact.ID,
			RecordedAtMs: fact.RecordedAt.UnixMilli(),
		}
	}
	if !ValidTrade(reply, value.StrategyID) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

// ValidTradePage binds each complete response to the original cursor and scope.
func ValidTradePage(value *strategyv1.ListStrategyTradesReply, filter domain.AdminTradeFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || len(value.Trades) > filter.Page.Limit || value.NextAfterId < 0 {
		return false
	}
	previous := filter.Page.AfterID
	for _, trade := range value.Trades {
		if !ValidTrade(trade, filter.StrategyID) || trade.Id <= previous ||
			filter.OrderID != 0 && (trade.OrderId == nil || trade.GetOrderId() != filter.OrderID) ||
			filter.Side != domain.SideUnspecified && trade.Side != string(filter.Side) {
			return false
		}
		previous = trade.Id
	}
	return value.NextAfterId == 0 || len(value.Trades) == filter.Page.Limit && value.NextAfterId == previous
}

// ValidTrade validates identity, exact amounts and optional collected-fact data.
func ValidTrade(value *strategyv1.StrategyTrade, strategyID int64) bool {
	if !validTradeIdentity(value, strategyID) {
		return false
	}
	price, priceErr := domain.ParseDecimal(value.Price, orderParameterRule(false))
	quantity, quantityErr := domain.ParseDecimal(value.Quantity, orderParameterRule(false))
	notional, notionalErr := domain.ParseLedgerDecimal(value.QuoteNotional, false, false)
	fee, feeErr := domain.ParseLedgerDecimal(value.FeeAmount, false, true)
	if priceErr != nil || quantityErr != nil || notionalErr != nil || feeErr != nil || !price.Mul(quantity).Equal(notional) || !fee.LessThan(notional) {
		return false
	}
	if fact := value.PortfolioFact; fact != nil {
		return value.OrderId != nil && len(fact.ProtoReflect().GetUnknown()) == 0 && fact.Id > 0 && fact.RecordedAtMs > 0
	}
	return true
}

func validTradeIdentity(value *strategyv1.StrategyTrade, strategyID int64) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && strategyID > 0 && value.StrategyId == strategyID &&
		value.Id > 0 && value.CoreOrderId > 0 && value.InstrumentId > 0 && value.ConfigVersion > 0 && value.RecordedAtMs > 0 &&
		value.ExecutionVenue == domain.ExecutionVenueSimulator && domain.MarketSource(value.MarketDataSource).ValidateSelection() == nil &&
		riskText(value.TradeId, 128) && riskText(value.AccountId, 64) && riskText(value.ClientOrderId, 64) && riskText(value.FeeAsset, 32) &&
		(value.OrderId == nil || value.GetOrderId() > 0) && (value.Side == string(domain.SideBuy) || value.Side == string(domain.SideSell))
}
