package executiongrpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func strategySubmitRequest(ctx context.Context, input domain.OrderInput) (*executionv1.SubmitIntentRequest, error) {
	if err := strategyControl(ctx, input.Authority, input.AccountID, input.IdempotencyKey, input.InstrumentID, input.Deadline); err != nil {
		return nil, err
	}
	if !domain.FixedDecimal(input.Price, 20, 18, false) || !domain.FixedDecimal(input.Quantity, 20, 18, false) ||
		input.RequestID != "" && !identifier(input.RequestID) || input.ReduceOnly && input.Side != domain.SideSell {
		return nil, domain.ErrInvalidOrder
	}
	var side commonv1.OrderSide
	switch input.Side {
	case domain.SideBuy:
		side = commonv1.OrderSide_ORDER_SIDE_BUY
	case domain.SideSell:
		side = commonv1.OrderSide_ORDER_SIDE_SELL
	default:
		return nil, domain.ErrInvalidOrder
	}
	var tif commonv1.TimeInForce
	switch input.TimeInForce {
	case domain.TimeInForceGTC:
		tif = commonv1.TimeInForce_TIME_IN_FORCE_GTC
	case domain.TimeInForcePostOnly:
		tif = commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY
	default:
		return nil, domain.ErrInvalidOrder
	}
	return &executionv1.SubmitIntentRequest{
		RequestId:      input.RequestID,
		IdempotencyKey: input.IdempotencyKey,
		Authority:      strategyAuthority(input.Authority),
		AccountId:      input.AccountID,
		InstrumentId:   input.InstrumentID,
		Side:           side,
		Price:          input.Price.String(),
		Quantity:       input.Quantity.String(),
		TimeInForce:    tif,
		ReduceOnly:     input.ReduceOnly,
		Deadline:       timestamppb.New(input.Deadline),
	}, nil
}

func strategyCancelRequest(ctx context.Context, input domain.CancelInput) (*executionv1.CancelOrderRequest, error) {
	if err := strategyControl(ctx, input.Authority, input.AccountID, input.IdempotencyKey, input.InstrumentID, input.Deadline); err != nil {
		return nil, err
	}
	if !identifier(input.ClientOrderID) || input.RequestID != "" && !identifier(input.RequestID) {
		return nil, domain.ErrInvalidOrder
	}
	return &executionv1.CancelOrderRequest{
		RequestId:      input.RequestID,
		IdempotencyKey: input.IdempotencyKey,
		Authority:      strategyAuthority(input.Authority),
		AccountId:      input.AccountID,
		InstrumentId:   input.InstrumentID,
		ClientOrderId:  input.ClientOrderID,
		Deadline:       timestamppb.New(input.Deadline),
	}, nil
}

func strategyControl(ctx context.Context, authority domain.StrategyAuthority, account, key string, instrumentID int64, deadline time.Time) error {
	if err := strategyQueryContext(ctx, account); err != nil {
		return err
	}
	native, _ := ctx.Deadline()
	if !boundedIdentifier(key, 128) || instrumentID <= 0 || authority.StrategyID <= 0 || authority.ConfigVersion <= 0 || authority.OwnerEpoch <= 0 ||
		!ownerid.Valid(authority.OwnerID) || !deadline.After(time.Now()) || deadline.After(native) || time.Until(deadline) > domain.MaxIntentLifetime {
		return domain.ErrInvalidOrder
	}
	return nil
}

func strategyAuthority(input domain.StrategyAuthority) *executionv1.StrategyAuthority {
	return &executionv1.StrategyAuthority{
		StrategyInstanceId: input.StrategyID,
		ConfigVersion:      input.ConfigVersion,
		OwnerId:            input.OwnerID,
		OwnerEpoch:         input.OwnerEpoch,
	}
}

func strategyCancellationState(input commonv1.CancellationState) (domain.CancellationState, error) {
	switch input {
	case commonv1.CancellationState_CANCELLATION_STATE_REQUESTED:
		return domain.CancellationRequested, nil
	case commonv1.CancellationState_CANCELLATION_STATE_SUBMITTING:
		return domain.CancellationSubmitting, nil
	case commonv1.CancellationState_CANCELLATION_STATE_PENDING:
		return domain.CancellationPending, nil
	case commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED:
		return domain.CancellationConfirmed, nil
	case commonv1.CancellationState_CANCELLATION_STATE_UNKNOWN:
		return domain.CancellationUnknown, nil
	case commonv1.CancellationState_CANCELLATION_STATE_REJECTED:
		return domain.CancellationRejected, nil
	default:
		return "", domain.ErrInvalidOrder
	}
}
