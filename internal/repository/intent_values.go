package repository

import (
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func intentSnapshot(row *modelv1.OrderIntent) (domain.IntentSnapshot, error) {
	price, err := decimal.NewFromString(row.Price)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	quantity, err := decimal.NewFromString(row.Quantity)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	filled, err := decimal.NewFromString(row.FilledQuantity)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	reserved, err := decimal.NewFromString(row.ReservedQuoteNotional)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	return domain.IntentSnapshot{
		ID:            row.Id,
		ClientOrderID: row.ClientOrderId,
		Input: domain.OrderInput{
			RequestID:      row.RequestId,
			IdempotencyKey: row.IdempotencyKey,
			Authority: domain.StrategyAuthority{
				StrategyID:    row.StrategyInstanceId,
				ConfigVersion: row.ConfigVersion,
				OwnerID:       row.OwnerId,
				OwnerEpoch:    row.OwnerEpoch,
			},
			AccountID:    row.AccountId,
			InstrumentID: row.InstrumentId,
			Side:         domain.Side(row.Side),
			Price:        price,
			Quantity:     quantity,
			TimeInForce:  domain.TimeInForce(row.TimeInForce),
			ReduceOnly:   row.ReduceOnly,
			Deadline:     time.UnixMilli(row.SubmissionDeadlineAt).UTC(),
		},
		ExecutionAuthority: domain.LeaseToken{
			Key: domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: row.AccountId,
			},
			OwnerID: row.ExecutionOwnerId,
			Epoch:   row.ExecutionEpoch,
		},
		State:                 domain.OrderState(row.State),
		CancelState:           domain.CancelState(row.CancelState),
		CoreOrderID:           row.GetCoreOrderId(),
		ReservedQuoteNotional: reserved,
		FilledQuantity:        filled,
		Revision:              row.Revision,
		ErrorCode:             row.ErrorCode,
	}, nil
}
