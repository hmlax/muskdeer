package strategywire

import (
	"math"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// CancellationCommand binds explicit order identity and its confirmed revision.
func CancellationCommand(input *strategyv1.OrderCancellationInput, strategyID, orderID int64, key string) (domain.AdminCancellationRequest, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return domain.AdminCancellationRequest{}, domain.ErrInvalidAdminCancellation
	}
	return (domain.AdminCancellationRequest{
		Key:                   key,
		StrategyID:            strategyID,
		OrderID:               orderID,
		ExpectedOrderRevision: input.ExpectedOrderRevision,
		Reason:                input.Reason,
	}).Normalize()
}

// ValidCancellationReceipt checks scoped identity and the complete result proof.
func ValidCancellationReceipt(value *strategyv1.OrderCancellationReceipt, strategyID, id int64) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || strategyID <= 0 || id <= 0 ||
		value.Id != id || value.StrategyId != strategyID || value.ActorUserId <= 0 || value.OrderId <= 0 ||
		value.ExpectedOrderRevision <= 0 || value.ExpectedOrderRevision == math.MaxInt64 || value.CreatedAtMs <= 0 || !riskText(value.Reason, 256) {
		return false
	}
	if domain.AdminCancellationState(value.State) == domain.AdminCancellationAccepted {
		return value.OutcomeCode == domain.AdminCancellationPending && value.CompletedAtMs == nil && value.Observation == nil &&
			(value.Revision == 1 || value.Revision == 2)
	}
	return validCancellationTerminal(value)
}

func validCancellationTerminal(value *strategyv1.OrderCancellationReceipt) bool {
	proof := value.Observation
	if value.CompletedAtMs == nil || *value.CompletedAtMs < value.CreatedAtMs || proof == nil || len(proof.ProtoReflect().GetUnknown()) != 0 ||
		proof.OrderRevision < value.ExpectedOrderRevision || !domain.ValidOrderState(domain.OrderState(proof.State)) ||
		(value.Revision != 2 && value.Revision != 3) {
		return false
	}
	switch domain.AdminCancellationState(value.State) {
	case domain.AdminCancellationSucceeded:
		return value.OutcomeCode == domain.AdminCancellationTerminal &&
			(proof.State == string(domain.OrderStateCanceled) || proof.State == string(domain.OrderStateFilled) || proof.State == string(domain.OrderStateRejected))
	case domain.AdminCancellationFailed:
		return (value.OutcomeCode == domain.AdminCancellationRejected || value.OutcomeCode == domain.AdminCancellationNotAccepted) &&
			openOrder(domain.OrderState(proof.State))
	default:
		return false
	}
}

// ValidCancellationResult additionally binds a mutation reply to its full input.
func ValidCancellationResult(value *strategyv1.OrderCancellationReceipt, request domain.AdminCancellationRequest) bool {
	return value != nil && ValidCancellationReceipt(value, request.StrategyID, value.Id) && value.OrderId == request.OrderID &&
		value.ExpectedOrderRevision == request.ExpectedOrderRevision && value.Reason == request.Reason
}
