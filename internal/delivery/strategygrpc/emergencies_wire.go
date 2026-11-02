package strategygrpc

import (
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

func emergencyAcceptanceProto(value domain.AdminEmergencyCancelAcceptance) (*strategyv1.EmergencyCancellationAcceptance, error) {
	if !overviewMillis(value.CreatedAt, false) {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.EmergencyCancellationAcceptance{
		Id:                   value.ID,
		ActorUserId:          value.ActorUserID,
		StrategyId:           value.StrategyID,
		Scope:                string(value.Scope),
		ExpectedRiskRevision: value.ExpectedRiskRevision,
		RiskRevision:         value.RiskRevision,
		Reason:               value.Reason,
		CreatedAtMs:          value.CreatedAt.UnixMilli(),
	}
	if !strategywire.ValidEmergencyAcceptance(reply, value.StrategyID, value.ID) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func emergencyRecordProto(value domain.AdminEmergencyCancelRecord) (*strategyv1.EmergencyCancellationRecord, error) {
	accepted, err := emergencyAcceptanceProto(value.Acceptance)
	if err != nil {
		return nil, err
	}
	reply := &strategyv1.EmergencyCancellationRecord{
		Acceptance:         accepted,
		State:              string(value.State),
		Revision:           value.Revision,
		EnrollmentComplete: value.EnrollmentComplete,
	}
	if !value.CompletedAt.IsZero() {
		if !overviewMillis(value.CompletedAt, false) {
			return nil, domain.ErrAdminUnavailable
		}
		reply.CompletedAtMs = proto.Int64(value.CompletedAt.UnixMilli())
	}
	if !strategywire.ValidEmergencyRecord(reply, accepted.StrategyId, accepted.Id) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func emergencyObservationProto(value domain.AdminEmergencyCancelObservation) (*strategyv1.EmergencyCancellationObservation, error) {
	if !overviewMillis(value.ObservedAt, false) {
		return nil, domain.ErrAdminUnavailable
	}
	record, err := emergencyRecordProto(value.Record)
	if err != nil {
		return nil, err
	}
	return &strategyv1.EmergencyCancellationObservation{
		Record: record,
		Progress: &strategyv1.EmergencyCancellationProgress{
			Targets:            value.Progress.Targets,
			Terminal:           value.Progress.Terminal,
			Failed:             value.Progress.Failed,
			PendingSubmissions: value.Progress.PendingSubmissions,
			EnrollmentComplete: value.Progress.EnrollmentComplete,
		},
		ObservedAtMs: value.ObservedAt.UnixMilli(),
	}, nil
}

func emergencyHistoryProto(value domain.AdminEmergencyCancelPage, limit int) (*strategyv1.ListEmergencyCancellationsReply, error) {
	if !overviewMillis(value.ObservedAt, false) || len(value.Items) > limit {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.ListEmergencyCancellationsReply{
		Items:        make([]*strategyv1.EmergencyCancellationRecord, 0, len(value.Items)),
		NextAfterId:  value.NextAfterID,
		ObservedAtMs: value.ObservedAt.UnixMilli(),
	}
	for _, item := range value.Items {
		record, err := emergencyRecordProto(item)
		if err != nil {
			return nil, err
		}
		reply.Items = append(reply.Items, record)
	}
	return reply, nil
}
