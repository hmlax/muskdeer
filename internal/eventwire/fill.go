// Package eventwire validates typed broker contracts before domain effects.
package eventwire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const messageInvalidEvent = "invalid execution event"

// ErrInvalidEvent is a permanent wire-contract failure, not a retry request.
var ErrInvalidEvent = errors.New(messageInvalidEvent)

// Fill checks the envelope, one concrete payload, enums and exact money syntax.
// It fingerprints deterministic Proto bytes without JSON/map conversion.
func Fill(ctx context.Context, event *commonv1.ExecutionEvent) (domain.FillNotice, error) {
	if ctx == nil {
		return domain.FillNotice{}, ErrInvalidEvent
	}
	if err := ctx.Err(); err != nil {
		return domain.FillNotice{}, err
	}
	if event == nil || event.Fill == nil || event.Intent != nil || event.Cancellation != nil || event.EventType != domain.EventTradeFilled ||
		event.Producer != domain.EventProducerExecution || event.SchemaVersion != domain.EventSchemaVersion || event.OccurredAtMs <= 0 ||
		event.AggregateId != event.Fill.SourceSequence || event.AggregateVersion != 1 || !identifier(event.EventId, 128) ||
		proto.Size(event) > 64*1024 || len(event.ProtoReflect().GetUnknown()) != 0 || len(event.Fill.ProtoReflect().GetUnknown()) != 0 {
		return domain.FillNotice{}, ErrInvalidEvent
	}
	fact, err := fillFact(event.Fill)
	if err != nil {
		return domain.FillNotice{}, err
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(event)
	if err != nil {
		return domain.FillNotice{}, ErrInvalidEvent
	}
	digest := sha256.Sum256(data)
	return domain.FillNotice{
		EventID:     event.EventId,
		PayloadHash: hex.EncodeToString(digest[:]),
		Fact:        fact,
	}, nil
}

func fillFact(payload *commonv1.FillEvent) (domain.FillFact, error) {
	if !identifier(payload.TradeId, 128) || !identifier(payload.AccountId, 64) || !identifier(payload.FeeAsset, 32) {
		return domain.FillFact{}, ErrInvalidEvent
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	price, err := domain.ParseDecimal(payload.Price, rule)
	if err != nil {
		return domain.FillFact{}, ErrInvalidEvent
	}
	quantity, err := domain.ParseDecimal(payload.Quantity, rule)
	if err != nil {
		return domain.FillFact{}, ErrInvalidEvent
	}
	notional, err := domain.ParseLedgerDecimal(payload.QuoteNotional, false, false)
	if err != nil {
		return domain.FillFact{}, ErrInvalidEvent
	}
	fee, err := domain.ParseLedgerDecimal(payload.FeeAmount, false, true)
	if err != nil {
		return domain.FillFact{}, ErrInvalidEvent
	}
	var side domain.Side
	switch payload.Side {
	case commonv1.OrderSide_ORDER_SIDE_BUY:
		side = domain.SideBuy
	case commonv1.OrderSide_ORDER_SIDE_SELL:
		side = domain.SideSell
	default:
		return domain.FillFact{}, ErrInvalidEvent
	}
	fact := domain.FillFact{
		ExecutionVenue: payload.ExecutionVenue,
		TradeID:        payload.TradeId,
		SourceSequence: payload.SourceSequence,
		IntentID:       payload.IntentId,
		CoreOrderID:    payload.CoreOrderId,
		AccountID:      payload.AccountId,
		StrategyID:     payload.StrategyInstanceId,
		InstrumentID:   payload.InstrumentId,
		Side:           side,
		Price:          price,
		Quantity:       quantity,
		QuoteNotional:  notional,
		FeeAmount:      fee,
		FeeAsset:       payload.FeeAsset,
	}
	if err := domain.ValidateFillFact(fact); err != nil {
		return domain.FillFact{}, ErrInvalidEvent
	}
	return fact, nil
}

func identifier(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		c := value[index]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}
