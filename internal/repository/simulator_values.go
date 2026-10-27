package repository

import (
	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func coreOrder(row *modelv1.SimulatorOrder) (domain.CoreOrder, error) {
	result := domain.CoreOrder{
		ID:            row.Id,
		ClientOrderID: row.ClientOrderId,
		AccountID:     row.AccountId,
		StrategyID:    row.StrategyInstanceId,
		ConfigVersion: row.ConfigVersion,
		InstrumentID:  row.InstrumentId,
		Side:          domain.Side(row.Side),
		TimeInForce:   domain.TimeInForce(row.TimeInForce),
		ReduceOnly:    row.ReduceOnly,
		State:         domain.OrderState(row.State),
		CancelState:   domain.CancelState(row.CancelState),
		FrozenAsset:   row.FrozenAsset,
		Revision:      row.Revision,
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
			text:   row.FilledQuantity,
			target: &result.FilledQuantity,
		},
		{
			text:   row.FrozenAmount,
			target: &result.FrozenAmount,
		},
		{
			text:   row.QuoteFeeRate,
			target: &result.QuoteFeeRate,
		},
	} {
		value, err := decimal.NewFromString(field.text)
		if err != nil {
			return domain.CoreOrder{}, &corePersistenceError{cause: err}
		}
		*field.target = value
	}
	return result, nil
}

func coreFill(row *modelv1.SimulatorFill) (domain.CoreFill, error) {
	price, err := decimal.NewFromString(row.Price)
	if err != nil {
		return domain.CoreFill{}, err
	}
	quantity, err := decimal.NewFromString(row.Quantity)
	if err != nil {
		return domain.CoreFill{}, err
	}
	notional, err := decimal.NewFromString(row.QuoteNotional)
	if err != nil {
		return domain.CoreFill{}, err
	}
	fee, err := decimal.NewFromString(row.FeeAmount)
	if err != nil {
		return domain.CoreFill{}, err
	}
	return domain.CoreFill{
		ID:            row.Id,
		CoreOrderID:   row.CoreOrderId,
		TradeID:       row.TradeId,
		Price:         price,
		Quantity:      quantity,
		QuoteNotional: notional,
		FeeAmount:     fee,
		FeeAsset:      row.FeeAsset,
	}, nil
}
