package strategywire

import (
	"testing"

	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func cancellationWireReceipt() *strategyv1.OrderCancellationReceipt {
	return &strategyv1.OrderCancellationReceipt{
		Id:                    9007199254740993,
		ActorUserId:           9007199254740994,
		StrategyId:            9007199254740995,
		OrderId:               9007199254740996,
		ExpectedOrderRevision: 9007199254740997,
		Reason:                "cancel selected order",
		State:                 string(domain.AdminCancellationAccepted),
		OutcomeCode:           domain.AdminCancellationPending,
		Revision:              1,
		CreatedAtMs:           1700000000000,
	}
}

func TestCancellationWireRetainsPresenceAndExplicitTerminalFacts(t *testing.T) {
	t.Parallel()
	value := cancellationWireReceipt()
	if !ValidCancellationReceipt(value, value.StrategyId, value.Id) {
		t.Fatal("valid scoped acceptance was rejected")
	}
	encoded, err := proto.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.OrderCancellationReceipt
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(value, &decoded) || decoded.CompletedAtMs != nil || decoded.Observation != nil {
		t.Fatal("protobuf lost exact identity or accepted presence", err)
	}
	for _, state := range []domain.OrderState{domain.OrderStateCanceled, domain.OrderStateFilled, domain.OrderStateRejected} {
		value.State, value.OutcomeCode, value.Revision = string(domain.AdminCancellationSucceeded), domain.AdminCancellationTerminal, 2
		value.CompletedAtMs = proto.Int64(value.CreatedAtMs)
		value.Observation = &strategyv1.OrderCancellationObservation{
			OrderRevision: value.ExpectedOrderRevision,
			State:         string(state),
		}
		if !ValidCancellationReceipt(value, value.StrategyId, value.Id) {
			t.Fatal("terminal objective erased a valid order fact", state)
		}
	}
	value.State, value.OutcomeCode, value.Observation.State = string(domain.AdminCancellationFailed), domain.AdminCancellationNotAccepted, string(domain.OrderStateUnknown)
	if !ValidCancellationReceipt(value, value.StrategyId, value.Id) {
		t.Fatal("explicit failure erased an unknown obligation")
	}
}

func TestCancellationWireRejectsIncompleteOrContradictoryProof(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*strategyv1.OrderCancellationReceipt){
		func(r *strategyv1.OrderCancellationReceipt) { r.OrderId = 0 },
		func(r *strategyv1.OrderCancellationReceipt) { r.ExpectedOrderRevision = 0 },
		func(r *strategyv1.OrderCancellationReceipt) { r.ActorUserId = 0 },
		func(r *strategyv1.OrderCancellationReceipt) { r.Reason = " noncanonical " },
		func(r *strategyv1.OrderCancellationReceipt) { r.Revision = 3 },
		func(r *strategyv1.OrderCancellationReceipt) { r.CompletedAtMs = proto.Int64(r.CreatedAtMs) },
		func(r *strategyv1.OrderCancellationReceipt) { r.State = string(domain.AdminCancellationSucceeded) },
		func(r *strategyv1.OrderCancellationReceipt) { r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1}) },
	} {
		value := cancellationWireReceipt()
		change(value)
		if ValidCancellationReceipt(value, value.StrategyId, value.Id) {
			t.Fatal("invalid cancellation acceptance escaped")
		}
	}
	for _, change := range []func(*strategyv1.OrderCancellationReceipt){
		func(r *strategyv1.OrderCancellationReceipt) { r.Observation = nil },
		func(r *strategyv1.OrderCancellationReceipt) { r.CompletedAtMs = nil },
		func(r *strategyv1.OrderCancellationReceipt) { r.CompletedAtMs = proto.Int64(r.CreatedAtMs - 1) },
		func(r *strategyv1.OrderCancellationReceipt) { r.Observation.OrderRevision-- },
		func(r *strategyv1.OrderCancellationReceipt) { r.Observation.State = string(domain.OrderStateLive) },
		func(r *strategyv1.OrderCancellationReceipt) { r.OutcomeCode = domain.AdminCancellationNotAccepted },
		func(r *strategyv1.OrderCancellationReceipt) {
			r.Observation.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
	} {
		value := cancellationWireReceipt()
		value.State, value.OutcomeCode, value.Revision = string(domain.AdminCancellationSucceeded), domain.AdminCancellationTerminal, 3
		value.CompletedAtMs = proto.Int64(value.CreatedAtMs + 1)
		value.Observation = &strategyv1.OrderCancellationObservation{
			OrderRevision: value.ExpectedOrderRevision,
			State:         string(domain.OrderStateCanceled),
		}
		change(value)
		if ValidCancellationReceipt(value, value.StrategyId, value.Id) {
			t.Fatal("incomplete or contradictory terminal cancellation escaped")
		}
	}
}
