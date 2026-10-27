// Package orderwire converts typed execution contracts into business values.
package orderwire

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Submit checks bounded lifetime and explicit decimal/enum/identity syntax.
// Instrument alignment, funds, risk and fencing require authoritative checks.
func Submit(ctx context.Context, input *executionv1.SubmitIntentRequest, now time.Time) (domain.OrderInput, error) {
	var output domain.OrderInput
	if ctx == nil || input == nil {
		return output, invalid("request")
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	if len(input.ProtoReflect().GetUnknown()) != 0 || !identifier(input.IdempotencyKey, 128) || !identifier(input.AccountId, 64) || input.InstrumentId <= 0 {
		return output, invalid("identity")
	}
	authority, err := decodeAuthority(input.Authority)
	if err != nil {
		return output, err
	}
	deadline, err := decodeDeadline(ctx, input.Deadline, now)
	if err != nil {
		return output, err
	}
	correlation, err := correlationID(ctx, input.RequestId)
	if err != nil {
		return output, err
	}
	output = domain.OrderInput{
		RequestID:      correlation,
		IdempotencyKey: input.IdempotencyKey,
		Authority:      authority,
		AccountID:      input.AccountId,
		InstrumentID:   input.InstrumentId,
		ReduceOnly:     input.ReduceOnly,
		Deadline:       deadline,
	}
	output.Side, err = decodeSide(input.Side)
	if err != nil {
		return domain.OrderInput{}, err
	}
	output.TimeInForce, err = decodeTimeInForce(input.TimeInForce)
	if err != nil {
		return domain.OrderInput{}, err
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	output.Price, err = domain.ParseDecimal(input.Price, rule)
	if err != nil {
		return domain.OrderInput{}, invalid("price")
	}
	output.Quantity, err = domain.ParseDecimal(input.Quantity, rule)
	if err != nil {
		return domain.OrderInput{}, invalid("quantity")
	}
	return output, nil
}

// Cancel parses an explicit order identity and current proposed owner token.
func Cancel(ctx context.Context, input *executionv1.CancelOrderRequest, now time.Time) (domain.CancelInput, error) {
	var output domain.CancelInput
	if ctx == nil || input == nil {
		return output, invalid("request")
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	if len(input.ProtoReflect().GetUnknown()) != 0 || !identifier(input.IdempotencyKey, 128) || !identifier(input.AccountId, 64) || !identifier(input.ClientOrderId, 64) || input.InstrumentId <= 0 {
		return output, invalid("identity")
	}
	authority, err := decodeAuthority(input.Authority)
	if err != nil {
		return output, err
	}
	deadline, err := decodeDeadline(ctx, input.Deadline, now)
	if err != nil {
		return output, err
	}
	correlation, err := correlationID(ctx, input.RequestId)
	if err != nil {
		return output, err
	}
	return domain.CancelInput{
		RequestID:      correlation,
		IdempotencyKey: input.IdempotencyKey,
		Authority:      authority,
		AccountID:      input.AccountId,
		InstrumentID:   input.InstrumentId,
		ClientOrderID:  input.ClientOrderId,
		Deadline:       deadline,
	}, nil
}

func decodeAuthority(input *executionv1.StrategyAuthority) (domain.StrategyAuthority, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.StrategyInstanceId <= 0 || input.ConfigVersion <= 0 || input.OwnerEpoch <= 0 || !identifier(input.OwnerId, 128) {
		return domain.StrategyAuthority{}, invalid("authority")
	}
	return domain.StrategyAuthority{
		StrategyID:    input.StrategyInstanceId,
		ConfigVersion: input.ConfigVersion,
		OwnerID:       input.OwnerId,
		OwnerEpoch:    input.OwnerEpoch,
	}, nil
}

func decodeDeadline(ctx context.Context, input *timestamppb.Timestamp, now time.Time) (time.Time, error) {
	if input.CheckValid() != nil {
		return time.Time{}, invalid("deadline")
	}
	deadline := input.AsTime()
	caller, bounded := ctx.Deadline()
	if !bounded || !deadline.After(now) || deadline.After(caller) || deadline.Sub(now) > domain.MaxIntentLifetime {
		return time.Time{}, invalid("deadline")
	}
	return deadline, nil
}

func correlationID(ctx context.Context, candidate string) (string, error) {
	if candidate != "" && !identifier(candidate, 64) {
		return "", invalid("request_id")
	}
	if trusted := requestid.FromContext(ctx).String(); trusted != "" {
		return trusted, nil
	}
	id, err := requestid.New(ctx, candidate)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func decodeSide(input commonv1.OrderSide) (domain.Side, error) {
	switch input {
	case commonv1.OrderSide_ORDER_SIDE_BUY:
		return domain.SideBuy, nil
	case commonv1.OrderSide_ORDER_SIDE_SELL:
		return domain.SideSell, nil
	default:
		return domain.SideUnspecified, invalid("side")
	}
}

func decodeTimeInForce(input commonv1.TimeInForce) (domain.TimeInForce, error) {
	switch input {
	case commonv1.TimeInForce_TIME_IN_FORCE_GTC:
		return domain.TimeInForceGTC, nil
	case commonv1.TimeInForce_TIME_IN_FORCE_IOC:
		return domain.TimeInForceIOC, nil
	case commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY:
		return domain.TimeInForcePostOnly, nil
	default:
		return domain.TimeInForceUnspecified, invalid("time_in_force")
	}
}

func identifier(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func invalid(field string) error { return &domain.OrderValidationError{Field: field} }
