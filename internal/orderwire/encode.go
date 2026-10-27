package orderwire

import (
	"strconv"

	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Order encodes exact decimal strings and rejects unknown business states.
func Order(input domain.IntentSnapshot) (*executionv1.OrderView, error) {
	if input.ID <= 0 || input.Revision <= 0 || input.CoreOrderID < 0 || input.CoreRevision < 0 ||
		!domain.FixedDecimal(input.Input.Price, 20, 18, false) || !domain.FixedDecimal(input.Input.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(input.FilledQuantity, 20, 18, true) || input.FilledQuantity.GreaterThan(input.Input.Quantity) {
		return nil, domain.ErrInvalidOrder
	}
	side, err := encodeSide(input.Input.Side)
	if err != nil {
		return nil, err
	}
	state, err := encodeState(input.State)
	if err != nil {
		return nil, err
	}
	cancellation, err := encodeCancelState(input.CancelState)
	if err != nil {
		return nil, err
	}
	tif, err := encodeTIF(input.Input.TimeInForce)
	if err != nil {
		return nil, err
	}
	updated := timestamppb.New(input.UpdatedAt)
	if updated.CheckValid() != nil {
		return nil, domain.ErrInvalidOrder
	}
	coreID := ""
	if input.CoreOrderID > 0 {
		coreID = strconv.FormatInt(input.CoreOrderID, 10)
	}
	return &executionv1.OrderView{
		IntentId:           input.ID,
		ClientOrderId:      input.ClientOrderID,
		CoreOrderId:        coreID,
		CoreRevision:       input.CoreRevision,
		ReduceOnly:         input.Input.ReduceOnly,
		ExecutionVenue:     domain.ExecutionVenueSimulator,
		AccountId:          input.Input.AccountID,
		InstrumentId:       input.Input.InstrumentID,
		StrategyInstanceId: input.Input.Authority.StrategyID,
		ConfigVersion:      input.Input.Authority.ConfigVersion,
		Side:               side,
		Price:              input.Input.Price.String(),
		Quantity:           input.Input.Quantity.String(),
		FilledQuantity:     input.FilledQuantity.String(),
		Notional:           input.Input.Price.Mul(input.Input.Quantity).String(),
		TimeInForce:        tif,
		State:              state,
		CancelState:        cancellation,
		Revision:           input.Revision,
		UpdatedAt:          updated,
		ErrorCode:          input.ErrorCode,
	}, nil
}

// Cancellation retains request state separately from the core order state.
func Cancellation(input domain.CancellationSnapshot) (*executionv1.CancellationView, error) {
	if input.ID <= 0 || input.IntentID <= 0 || input.Revision <= 0 {
		return nil, domain.ErrInvalidOrder
	}
	var state commonv1.CancellationState
	switch input.State {
	case domain.CancellationRequested:
		state = commonv1.CancellationState_CANCELLATION_STATE_REQUESTED
	case domain.CancellationSubmitting:
		state = commonv1.CancellationState_CANCELLATION_STATE_SUBMITTING
	case domain.CancellationPending:
		state = commonv1.CancellationState_CANCELLATION_STATE_PENDING
	case domain.CancellationConfirmed:
		state = commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED
	case domain.CancellationUnknown:
		state = commonv1.CancellationState_CANCELLATION_STATE_UNKNOWN
	case domain.CancellationRejected:
		state = commonv1.CancellationState_CANCELLATION_STATE_REJECTED
	default:
		return nil, domain.ErrInvalidOrder
	}
	return &executionv1.CancellationView{
		Id:        input.ID,
		IntentId:  input.IntentID,
		State:     state,
		Revision:  input.Revision,
		ErrorCode: input.ErrorCode,
	}, nil
}

// Fill exposes immutable source fields without parsing through a float.
func Fill(input domain.ExecutionFill) (*executionv1.FillView, error) {
	fill := input.Fill
	if fill.ID <= 0 || !domain.FixedDecimal(fill.Price, 20, 18, false) || !domain.FixedDecimal(fill.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(fill.QuoteNotional, 40, 36, false) || !domain.FixedDecimal(fill.FeeAmount, 40, 36, true) ||
		!fill.QuoteNotional.Equal(fill.Price.Mul(fill.Quantity)) {
		return nil, domain.ErrInvalidOrder
	}
	side, err := encodeSide(input.Side)
	if err != nil {
		return nil, err
	}
	occurred := timestamppb.New(input.OccurredAt)
	if occurred.CheckValid() != nil {
		return nil, domain.ErrInvalidOrder
	}
	return &executionv1.FillView{
		Id:                 fill.ID,
		TradeId:            fill.TradeID,
		ClientOrderId:      input.ClientOrderID,
		ExecutionVenue:     domain.ExecutionVenueSimulator,
		AccountId:          input.AccountID,
		InstrumentId:       input.InstrumentID,
		StrategyInstanceId: input.StrategyID,
		Side:               side,
		Price:              fill.Price.String(),
		Quantity:           fill.Quantity.String(),
		QuoteNotional:      fill.QuoteNotional.String(),
		FeeAmount:          fill.FeeAmount.String(),
		FeeAsset:           fill.FeeAsset,
		Sequence:           fill.ID,
		OccurredAt:         occurred,
	}, nil
}

// Account encodes one consistent source revision and a bounded asset list.
func Account(input domain.ExecutionAccount) (*executionv1.AccountReply, error) {
	if input.Sequence <= 0 || !identifier(input.AccountID, 64) || len(input.Balances) > domain.MaximumExecutionAssets {
		return nil, domain.ErrInvalidOrder
	}
	observed := timestamppb.New(input.ObservedAt)
	if observed.CheckValid() != nil {
		return nil, domain.ErrInvalidOrder
	}
	result := &executionv1.AccountReply{
		AccountId:      input.AccountID,
		ExecutionVenue: domain.ExecutionVenueSimulator,
		Sequence:       input.Sequence,
		ObservedAt:     observed,
		Balances:       make([]*executionv1.AssetBalance, 0, len(input.Balances)),
	}
	for _, balance := range input.Balances {
		if !identifier(balance.Asset, 32) || !domain.FixedDecimal(balance.Available, 40, 36, true) || !domain.FixedDecimal(balance.Frozen, 40, 36, true) {
			return nil, domain.ErrInvalidOrder
		}
		result.Balances = append(result.Balances, &executionv1.AssetBalance{
			Asset:     balance.Asset,
			Available: balance.Available.String(),
			Frozen:    balance.Frozen.String(),
		})
	}
	return result, nil
}

func encodeSide(side domain.Side) (commonv1.OrderSide, error) {
	switch side {
	case domain.SideBuy:
		return commonv1.OrderSide_ORDER_SIDE_BUY, nil
	case domain.SideSell:
		return commonv1.OrderSide_ORDER_SIDE_SELL, nil
	default:
		return commonv1.OrderSide_ORDER_SIDE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}

func encodeState(state domain.OrderState) (commonv1.OrderState, error) {
	switch state {
	case domain.OrderStateIntent:
		return commonv1.OrderState_ORDER_STATE_INTENT, nil
	case domain.OrderStateSubmitting:
		return commonv1.OrderState_ORDER_STATE_SUBMITTING, nil
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
	case domain.OrderStateUnknown:
		return commonv1.OrderState_ORDER_STATE_UNKNOWN, nil
	default:
		return commonv1.OrderState_ORDER_STATE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}

func encodeCancelState(state domain.CancelState) (commonv1.CancelState, error) {
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

func encodeTIF(tif domain.TimeInForce) (commonv1.TimeInForce, error) {
	switch tif {
	case domain.TimeInForceGTC:
		return commonv1.TimeInForce_TIME_IN_FORCE_GTC, nil
	case domain.TimeInForceIOC:
		return commonv1.TimeInForce_TIME_IN_FORCE_IOC, nil
	case domain.TimeInForcePostOnly:
		return commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY, nil
	default:
		return commonv1.TimeInForce_TIME_IN_FORCE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}
