package repository

import (
	"context"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *intentStore) Confirm(ctx context.Context, current domain.IntentSnapshot, order domain.CoreOrder) (domain.IntentSnapshot, error) {
	if order.ID <= 0 || order.Revision <= 0 || !validCoreProgress(order) {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return s.changeIntent(ctx, current, func(row *modelv1.OrderIntent) (bool, error) {
		stored, err := intentSnapshot(row)
		if err != nil {
			return false, err
		}
		if !sameCoreBusiness(stored, order) || (stored.CoreOrderID != 0 && stored.CoreOrderID != order.ID) {
			return false, domain.ErrInvalidOrder
		}
		if order.Revision < row.CoreRevision {
			return false, nil
		}
		if row.CoreRevision > 0 && order.State == domain.OrderStateRejected && row.State != string(domain.OrderStateRejected) {
			return false, domain.ErrInvalidOrder
		}
		if order.FilledQuantity.LessThan(stored.FilledQuantity) || (terminalIntent(row.State) && row.State != string(order.State)) {
			return false, domain.ErrInvalidOrder
		}
		if order.Revision == row.CoreRevision {
			if row.CancelState != string(order.CancelState) || !order.FilledQuantity.Equal(stored.FilledQuantity) {
				return false, domain.ErrInvalidOrder
			}
			if row.State == string(order.State) {
				return false, nil
			}
			if row.State != string(domain.OrderStateUnknown) || (order.State != domain.OrderStateLive && order.State != domain.OrderStatePartiallyFilled) {
				return false, domain.ErrInvalidOrder
			}
		}
		row.CoreOrderId = &order.ID
		row.CoreRevision = order.Revision
		row.State = string(order.State)
		row.CancelState = string(order.CancelState)
		row.FilledQuantity = order.FilledQuantity.String()
		row.ErrorCode = ""
		row.ReservedQuoteNotional = stored.Input.Price.Mul(stored.Input.Quantity.Sub(order.FilledQuantity)).String()
		if terminalIntent(row.State) {
			row.ReservedQuoteNotional = "0"
		}
		return true, nil
	})
}

func sameCoreBusiness(intent domain.IntentSnapshot, order domain.CoreOrder) bool {
	in := intent.Input
	return order.ClientOrderID == intent.ClientOrderID && order.AccountID == in.AccountID && order.StrategyID == in.Authority.StrategyID &&
		order.ConfigVersion == in.Authority.ConfigVersion && order.InstrumentID == in.InstrumentID && order.Side == in.Side &&
		order.Price.Equal(in.Price) && order.Quantity.Equal(in.Quantity) && order.TimeInForce == in.TimeInForce && order.ReduceOnly == in.ReduceOnly
}

func validCoreProgress(order domain.CoreOrder) bool {
	if !domain.FixedDecimal(order.Price, 20, 18, false) || !domain.FixedDecimal(order.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(order.FilledQuantity, 20, 18, true) || order.FilledQuantity.GreaterThan(order.Quantity) {
		return false
	}
	if _, err := intentWireCancelState(order.CancelState); err != nil {
		return false
	}
	switch order.State {
	case domain.OrderStateLive, domain.OrderStateRejected:
		return order.FilledQuantity.IsZero()
	case domain.OrderStatePartiallyFilled:
		return order.FilledQuantity.GreaterThan(decimal.Zero) && order.FilledQuantity.LessThan(order.Quantity)
	case domain.OrderStateFilled:
		return order.FilledQuantity.Equal(order.Quantity)
	case domain.OrderStateCanceled:
		return order.FilledQuantity.LessThan(order.Quantity)
	default:
		return false
	}
}
