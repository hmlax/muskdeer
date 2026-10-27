package orderwire

import (
	"strconv"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Snapshot decodes an observation without manufacturing an owner or idempotency
// key. It cannot be reused as an authorized submission command.
func Snapshot(input *executionv1.OrderView) (domain.IntentSnapshot, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.UpdatedAt.CheckValid() != nil || len(input.UpdatedAt.ProtoReflect().GetUnknown()) != 0 ||
		input.ExecutionVenue != domain.ExecutionVenueSimulator || input.IntentId <= 0 || input.Revision <= 0 || input.CoreRevision < 0 ||
		!identifier(input.AccountId, 64) || !identifier(input.ClientOrderId, 64) || input.InstrumentId <= 0 || input.StrategyInstanceId <= 0 || input.ConfigVersion <= 0 {
		return domain.IntentSnapshot{}, invalid("observation")
	}
	output := domain.IntentSnapshot{
		ID:            input.IntentId,
		ClientOrderID: input.ClientOrderId,
		Input: domain.OrderInput{
			AccountID:    input.AccountId,
			InstrumentID: input.InstrumentId,
			Authority: domain.StrategyAuthority{
				StrategyID:    input.StrategyInstanceId,
				ConfigVersion: input.ConfigVersion,
			},
			ReduceOnly: input.ReduceOnly,
		},
		Revision:     input.Revision,
		CoreRevision: input.CoreRevision,
		UpdatedAt:    input.UpdatedAt.AsTime().UTC(),
		ErrorCode:    input.ErrorCode,
	}
	var err error
	if input.CoreOrderId != "" {
		output.CoreOrderID, err = strconv.ParseInt(input.CoreOrderId, 10, 64)
		if err != nil || output.CoreOrderID <= 0 || strconv.FormatInt(output.CoreOrderID, 10) != input.CoreOrderId || input.CoreRevision <= 0 {
			return domain.IntentSnapshot{}, invalid("observation")
		}
	}
	if output.Input.Side, err = decodeSide(input.Side); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if output.Input.TimeInForce, err = decodeTimeInForce(input.TimeInForce); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if output.State, err = decodeState(input.State); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if output.CancelState, err = decodeCancelState(input.CancelState); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if err := observationMoney(input, &output); err != nil {
		return domain.IntentSnapshot{}, err
	}
	return output, nil
}

func observationMoney(input *executionv1.OrderView, output *domain.IntentSnapshot) error {
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	var err error
	if output.Input.Price, err = domain.ParseDecimal(input.Price, rule); err != nil {
		return invalid("observation")
	}
	if output.Input.Quantity, err = domain.ParseDecimal(input.Quantity, rule); err != nil {
		return invalid("observation")
	}
	rule.AllowZero = true
	if output.FilledQuantity, err = domain.ParseDecimal(input.FilledQuantity, rule); err != nil || output.FilledQuantity.GreaterThan(output.Input.Quantity) {
		return invalid("observation")
	}
	notional, err := domain.ParseLedgerDecimal(input.Notional, false, false)
	if err != nil || !notional.Equal(output.Input.Price.Mul(output.Input.Quantity)) {
		return invalid("observation")
	}
	if output.CoreOrderID == 0 && (output.CoreRevision != 0 || !output.FilledQuantity.IsZero() || output.State == domain.OrderStateLive || output.State == domain.OrderStatePartiallyFilled || output.State == domain.OrderStateFilled) {
		return invalid("observation")
	}
	return nil
}

func decodeState(input commonv1.OrderState) (domain.OrderState, error) {
	switch input {
	case commonv1.OrderState_ORDER_STATE_INTENT:
		return domain.OrderStateIntent, nil
	case commonv1.OrderState_ORDER_STATE_SUBMITTING:
		return domain.OrderStateSubmitting, nil
	case commonv1.OrderState_ORDER_STATE_LIVE:
		return domain.OrderStateLive, nil
	case commonv1.OrderState_ORDER_STATE_PARTIALLY_FILLED:
		return domain.OrderStatePartiallyFilled, nil
	case commonv1.OrderState_ORDER_STATE_FILLED:
		return domain.OrderStateFilled, nil
	case commonv1.OrderState_ORDER_STATE_CANCELED:
		return domain.OrderStateCanceled, nil
	case commonv1.OrderState_ORDER_STATE_REJECTED:
		return domain.OrderStateRejected, nil
	case commonv1.OrderState_ORDER_STATE_UNKNOWN:
		return domain.OrderStateUnknown, nil
	default:
		return domain.OrderStateUnspecified, invalid("observation")
	}
}

func decodeCancelState(input commonv1.CancelState) (domain.CancelState, error) {
	switch input {
	case commonv1.CancelState_CANCEL_STATE_NONE:
		return domain.CancelStateNone, nil
	case commonv1.CancelState_CANCEL_STATE_REQUESTED:
		return domain.CancelStateRequested, nil
	case commonv1.CancelState_CANCEL_STATE_PENDING:
		return domain.CancelStatePending, nil
	case commonv1.CancelState_CANCEL_STATE_CONFIRMED:
		return domain.CancelStateConfirmed, nil
	case commonv1.CancelState_CANCEL_STATE_UNKNOWN:
		return domain.CancelStateUnknown, nil
	case commonv1.CancelState_CANCEL_STATE_REJECTED:
		return domain.CancelStateRejected, nil
	default:
		return domain.CancelStateUnspecified, invalid("observation")
	}
}
