package repository

import (
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func intentWireState(state domain.OrderState) (commonv1.OrderState, error) {
	switch state {
	case domain.OrderStateIntent:
		return commonv1.OrderState_ORDER_STATE_INTENT, nil
	case domain.OrderStateSubmitting:
		return commonv1.OrderState_ORDER_STATE_SUBMITTING, nil
	case domain.OrderStateUnknown:
		return commonv1.OrderState_ORDER_STATE_UNKNOWN, nil
	case domain.OrderStateLive:
		return commonv1.OrderState_ORDER_STATE_LIVE, nil
	case domain.OrderStatePartiallyFilled:
		return commonv1.OrderState_ORDER_STATE_PARTIALLY_FILLED, nil
	case domain.OrderStateFilled:
		return commonv1.OrderState_ORDER_STATE_FILLED, nil
	case domain.OrderStateCanceled:
		return commonv1.OrderState_ORDER_STATE_CANCELED, nil
	case domain.OrderStateRejected:
		return commonv1.OrderState_ORDER_STATE_REJECTED, nil
	default:
		return commonv1.OrderState_ORDER_STATE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}

func intentWireCancelState(state domain.CancelState) (commonv1.CancelState, error) {
	switch state {
	case domain.CancelStateNone:
		return commonv1.CancelState_CANCEL_STATE_NONE, nil
	case domain.CancelStateRequested:
		return commonv1.CancelState_CANCEL_STATE_REQUESTED, nil
	case domain.CancelStatePending:
		return commonv1.CancelState_CANCEL_STATE_PENDING, nil
	case domain.CancelStateConfirmed:
		return commonv1.CancelState_CANCEL_STATE_CONFIRMED, nil
	case domain.CancelStateUnknown:
		return commonv1.CancelState_CANCEL_STATE_UNKNOWN, nil
	case domain.CancelStateRejected:
		return commonv1.CancelState_CANCEL_STATE_REJECTED, nil
	default:
		return commonv1.CancelState_CANCEL_STATE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}
