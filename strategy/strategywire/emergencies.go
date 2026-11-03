package strategywire

import (
	"math"

	strategyv1 "github.com/hmlax/muskdeer/deer/proto/rpc/strategy/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// EmergencyCommand only accepts the explicitly confirmed strategy scope.
func EmergencyCommand(input *strategyv1.EmergencyCancellationInput, strategyID int64, key string) (domain.AdminEmergencyCancelRequest, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return domain.AdminEmergencyCancelRequest{}, domain.ErrInvalidAdminEmergencyCancel
	}
	return (domain.AdminEmergencyCancelRequest{
		Key:                  key,
		Scope:                domain.AdminEmergencyCancelScope(input.Scope),
		StrategyID:           strategyID,
		ExpectedRiskRevision: input.ExpectedRiskRevision,
		Reason:               input.Reason,
	}).Normalize()
}

// ValidEmergencyAcceptance checks immutable scoped risk-closure evidence.
func ValidEmergencyAcceptance(value *strategyv1.EmergencyCancellationAcceptance, strategyID, id int64) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && strategyID > 0 && id > 0 && value.Id == id && value.StrategyId == strategyID &&
		value.ActorUserId > 0 && value.Scope == string(domain.AdminEmergencyCancelScopeStrategy) && value.ExpectedRiskRevision > 0 &&
		value.ExpectedRiskRevision < math.MaxInt64 && value.RiskRevision == value.ExpectedRiskRevision+1 && riskText(value.Reason, 256) && value.CreatedAtMs > 0
}

// ValidEmergencyResult also binds the complete normalized request confirmation.
func ValidEmergencyResult(value *strategyv1.EmergencyCancellationAcceptance, request domain.AdminEmergencyCancelRequest) bool {
	return value != nil && ValidEmergencyAcceptance(value, request.StrategyID, value.Id) && value.Scope == string(request.Scope) &&
		value.ExpectedRiskRevision == request.ExpectedRiskRevision && value.Reason == request.Reason
}

// ValidEmergencyRecord checks stored proof independently of current counters.
func ValidEmergencyRecord(value *strategyv1.EmergencyCancellationRecord, strategyID, id int64) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || !ValidEmergencyAcceptance(value.Acceptance, strategyID, id) || value.Revision <= 0 {
		return false
	}
	switch domain.AdminEmergencyCancelState(value.State) {
	case domain.AdminEmergencyCancelAccepted:
		return value.CompletedAtMs == nil
	case domain.AdminEmergencyCancelSucceeded, domain.AdminEmergencyCancelFailed:
		return value.EnrollmentComplete && value.CompletedAtMs != nil && *value.CompletedAtMs >= value.Acceptance.CreatedAtMs
	default:
		return false
	}
}

// ValidEmergencyObservation never recomputes a historical state from counters.
func ValidEmergencyObservation(value *strategyv1.EmergencyCancellationObservation, strategyID, id int64) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || !ValidEmergencyRecord(value.Record, strategyID, id) ||
		!emergencyObserved(value.Record, value.ObservedAtMs) || value.Progress == nil || len(value.Progress.ProtoReflect().GetUnknown()) != 0 ||
		value.Progress.EnrollmentComplete != value.Record.EnrollmentComplete {
		return false
	}
	_, err := (domain.AdminEmergencyCancelProgress{
		Targets:            value.Progress.Targets,
		Terminal:           value.Progress.Terminal,
		Failed:             value.Progress.Failed,
		PendingSubmissions: value.Progress.PendingSubmissions,
		EnrollmentComplete: value.Progress.EnrollmentComplete,
	}).State()
	return err == nil
}

// ValidEmergencyHistory binds a bounded ordered page to its strategy and cursor.
func ValidEmergencyHistory(value *strategyv1.ListEmergencyCancellationsReply, strategyID int64, page domain.AdminPage) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || strategyID <= 0 || page.AfterID < 0 || page.Limit < 1 || page.Limit > domain.MaxAdminPageSize ||
		value.ObservedAtMs <= 0 || value.NextAfterId < 0 || len(value.Items) > page.Limit {
		return false
	}
	previous := page.AfterID
	for _, record := range value.Items {
		if record == nil || record.Acceptance == nil || !ValidEmergencyRecord(record, strategyID, record.Acceptance.Id) ||
			record.Acceptance.Id <= previous || !emergencyObserved(record, value.ObservedAtMs) {
			return false
		}
		previous = record.Acceptance.Id
	}
	return value.NextAfterId == 0 || len(value.Items) == page.Limit && value.NextAfterId == previous
}

func emergencyObserved(record *strategyv1.EmergencyCancellationRecord, observed int64) bool {
	return observed >= record.Acceptance.CreatedAtMs && (record.CompletedAtMs == nil || *record.CompletedAtMs <= observed)
}
