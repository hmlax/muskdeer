package repository

import (
	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func portfolioSnapshot(row *modelv1.PortfolioAccount) (domain.PortfolioSnapshot, error) {
	result := domain.PortfolioSnapshot{
		AccountID:    row.AccountId,
		InstrumentID: row.InstrumentId,
		BaseAsset:    row.BaseAsset,
		QuoteAsset:   row.QuoteAsset,
		Opening: domain.PortfolioOpening{
			AccountID:    row.AccountId,
			InstrumentID: row.InstrumentId,
		},
		LastSourceSequence: row.LastSourceSequence,
		Reconciled:         row.Reconciled,
		Revision:           row.Revision,
		ErrorCode:          row.ErrorCode,
	}
	for _, field := range []struct {
		text   string
		target *decimal.Decimal
	}{
		{
			text:   row.OpeningQuantity,
			target: &result.Opening.Quantity,
		},
		{
			text:   row.OpeningCashQuote,
			target: &result.Opening.CashQuote,
		},
		{
			text:   row.OpeningCostQuote,
			target: &result.Opening.CostQuote,
		},
		{
			text:   row.Quantity,
			target: &result.Values.Quantity,
		},
		{
			text:   row.CashQuote,
			target: &result.Values.CashQuote,
		},
		{
			text:   row.CostQuote,
			target: &result.Values.CostQuote,
		},
		{
			text:   row.RealizedPnlQuote,
			target: &result.Values.RealizedPnlQuote,
		},
		{
			text:   row.FeesQuote,
			target: &result.Values.FeesQuote,
		},
	} {
		value, err := decimal.NewFromString(field.text)
		if err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		*field.target = value
	}
	return result, nil
}

func portfolioFill(row *modelv1.PortfolioFill) (domain.FillFact, error) {
	result := domain.FillFact{
		ExecutionVenue: row.ExecutionVenue,
		TradeID:        row.TradeId,
		SourceSequence: row.SourceSequence,
		IntentID:       row.IntentId,
		CoreOrderID:    row.CoreOrderId,
		AccountID:      row.AccountId,
		StrategyID:     row.StrategyInstanceId,
		InstrumentID:   row.InstrumentId,
		Side:           domain.Side(row.Side),
		FeeAsset:       row.FeeAsset,
	}
	for _, field := range []struct {
		text   string
		target *decimal.Decimal
	}{
		{
			text:   row.Price,
			target: &result.Price,
		},
		{
			text:   row.Quantity,
			target: &result.Quantity,
		},
		{
			text:   row.QuoteNotional,
			target: &result.QuoteNotional,
		},
		{
			text:   row.FeeAmount,
			target: &result.FeeAmount,
		},
	} {
		value, err := decimal.NewFromString(field.text)
		if err != nil {
			return domain.FillFact{}, err
		}
		*field.target = value
	}
	return result, nil
}
