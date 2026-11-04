package strategywire

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/deer/pb/strategy/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestEmergencyWireKeepsHistoricalFailureSeparateFromCurrentCounters(t *testing.T) {
	value := emergencyWireObservation()
	value.Progress.Terminal, value.Progress.Failed = 1, 0
	if !ValidEmergencyObservation(value, value.Record.Acceptance.StrategyId, value.Record.Acceptance.Id) {
		t.Fatal("later terminal target erased a historical failure")
	}
	value.Record.State = string(domain.AdminEmergencyCancelAccepted)
	value.Record.CompletedAtMs = nil
	value.Record.EnrollmentComplete, value.Progress.EnrollmentComplete = false, false
	value.Progress.Targets, value.Progress.Terminal = 0, 0
	if !ValidEmergencyObservation(value, value.Record.Acceptance.StrategyId, value.Record.Acceptance.Id) {
		t.Fatal("unenrolled empty scope was required to claim completion")
	}
}

func TestEmergencyWireRejectsCorruptNestedProofCountersAndScope(t *testing.T) {
	for _, change := range []func(*strategyv1.EmergencyCancellationObservation){
		func(r *strategyv1.EmergencyCancellationObservation) { r.Record.Acceptance.StrategyId++ },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Record.Acceptance.Scope = "account" },
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.Record.Acceptance.ExpectedRiskRevision = math.MaxInt64
		},
		func(r *strategyv1.EmergencyCancellationObservation) { r.Record.Acceptance.RiskRevision++ },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Record.CompletedAtMs = nil },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Record.EnrollmentComplete = false },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Progress.Targets = -1 },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Progress.Terminal = 1 },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Progress.PendingSubmissions = -1 },
		func(r *strategyv1.EmergencyCancellationObservation) { r.Progress.EnrollmentComplete = false },
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.ObservedAtMs = r.Record.Acceptance.CreatedAtMs - 1
		},
		func(r *strategyv1.EmergencyCancellationObservation) { r.Progress = nil },
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.Record.Acceptance.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.Record.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.Progress.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		func(r *strategyv1.EmergencyCancellationObservation) {
			r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
	} {
		value := emergencyWireObservation()
		change(value)
		if ValidEmergencyObservation(value, 9007199254740995, 9007199254740993) {
			t.Fatal("corrupt emergency proof escaped typed validation")
		}
	}
}

func TestEmergencyWireBindsHistoryCursorAndObservationTime(t *testing.T) {
	record := emergencyWireObservation().Record
	value := &strategyv1.ListEmergencyCancellationsReply{
		Items:        []*strategyv1.EmergencyCancellationRecord{record},
		NextAfterId:  record.Acceptance.Id,
		ObservedAtMs: *record.CompletedAtMs,
	}
	page := domain.AdminPage{Limit: 1}
	if !ValidEmergencyHistory(value, record.Acceptance.StrategyId, page) {
		t.Fatal("valid high-identity history was truncated")
	}
	for _, change := range []func(*strategyv1.ListEmergencyCancellationsReply){
		func(r *strategyv1.ListEmergencyCancellationsReply) { r.NextAfterId++ },
		func(r *strategyv1.ListEmergencyCancellationsReply) { r.ObservedAtMs-- },
		func(r *strategyv1.ListEmergencyCancellationsReply) { r.Items = append(r.Items, proto.CloneOf(record)) },
		func(r *strategyv1.ListEmergencyCancellationsReply) { r.Items[0].Acceptance.Id = 0 },
		func(r *strategyv1.ListEmergencyCancellationsReply) { r.Items[0] = nil },
	} {
		altered := proto.CloneOf(value)
		change(altered)
		if ValidEmergencyHistory(altered, record.Acceptance.StrategyId, page) {
			t.Fatal("history escaped cursor, size, proof or observation bounds")
		}
	}
}

func emergencyWireObservation() *strategyv1.EmergencyCancellationObservation {
	return &strategyv1.EmergencyCancellationObservation{
		Record: &strategyv1.EmergencyCancellationRecord{
			Acceptance: &strategyv1.EmergencyCancellationAcceptance{
				Id:                   9007199254740993,
				ActorUserId:          9007199254740997,
				StrategyId:           9007199254740995,
				Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
				ExpectedRiskRevision: 9007199254740999,
				RiskRevision:         9007199254741000,
				Reason:               "confirmed strategy scope",
				CreatedAtMs:          1700000000000,
			},
			State:              string(domain.AdminEmergencyCancelFailed),
			Revision:           9007199254741001,
			EnrollmentComplete: true,
			CompletedAtMs:      proto.Int64(1700000000001),
		},
		Progress: &strategyv1.EmergencyCancellationProgress{
			Targets:            1,
			Failed:             1,
			EnrollmentComplete: true,
		},
		ObservedAtMs: 1700000000002,
	}
}
