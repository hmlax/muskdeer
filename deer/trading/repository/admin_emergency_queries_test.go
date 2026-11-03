package repository

import (
	"errors"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestAdminEmergencyRecordPreservesLargeIdentitiesAndStoredProof(t *testing.T) {
	accepted, task := emergencyQueryRows()
	record, err := adminEmergencyRecord(accepted, task)
	if err != nil || record.Acceptance.ID != 9007199254740993 || record.Acceptance.ActorUserID != 9007199254740995 || record.Acceptance.StrategyID != 9007199254740997 ||
		record.Revision != 9007199254740999 || record.Acceptance.ExpectedRiskRevision != 9007199254741001 || record.Acceptance.RiskRevision != 9007199254741002 ||
		record.State != domain.AdminEmergencyCancelAccepted || record.EnrollmentComplete || !record.CompletedAt.IsZero() {
		t.Fatal("query record changed exact identity or invented completion", err)
	}
	task.State = string(domain.AdminEmergencyCancelFailed)
	task.EnrollmentComplete = true
	task.CompletedAt = proto.Int64(accepted.CreateAt + 100)
	failed, err := adminEmergencyRecord(accepted, task)
	if err != nil || failed.State != domain.AdminEmergencyCancelFailed || !failed.EnrollmentComplete || failed.CompletedAt.UnixMilli() != *task.CompletedAt {
		t.Fatal("query record derived a new result instead of stored failure", err)
	}
}

func TestAdminEmergencyRecordRejectsMissingAndInconsistentProof(t *testing.T) {
	for _, change := range []func(*modelv1.AdminEmergencyCancellation, *modelv1.AdminEmergencyCancellationTask){
		func(row *modelv1.AdminEmergencyCancellation, _ *modelv1.AdminEmergencyCancellationTask) {
			row.ExpectedRiskRevision = math.MaxInt64
		},
		func(row *modelv1.AdminEmergencyCancellation, _ *modelv1.AdminEmergencyCancellationTask) {
			row.RiskRevision++
		},
		func(row *modelv1.AdminEmergencyCancellation, _ *modelv1.AdminEmergencyCancellationTask) {
			row.Scope = "account"
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.StrategyInstanceId++
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.EmergencyCancellationId++
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.State = "unknown-private-state"
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.Revision = 0
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.CompletedAt = proto.Int64(1)
		},
		func(_ *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.State = string(domain.AdminEmergencyCancelSucceeded)
		},
		func(row *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) {
			task.State = string(domain.AdminEmergencyCancelSucceeded)
			task.EnrollmentComplete = true
			task.CompletedAt = proto.Int64(row.CreateAt - 1)
		},
	} {
		row, task := emergencyQueryRows()
		change(row, task)
		if value, err := adminEmergencyRecord(row, task); !errors.Is(err, domain.ErrAdminUnavailable) || value.Acceptance.ID != 0 {
			t.Fatal("inconsistent proof produced partial public history", err)
		}
	}
	row, task := emergencyQueryRows()
	if value, err := adminEmergencyRecord(row, nil); !errors.Is(err, domain.ErrAdminUnavailable) || value.Acceptance.ID != 0 {
		t.Fatal("missing task became assumed completion", err)
	}
	if value, err := adminEmergencyRecord(nil, task); !errors.Is(err, domain.ErrAdminUnavailable) || value.Acceptance.ID != 0 {
		t.Fatal("missing acceptance became public proof", err)
	}
}

func emergencyQueryRows() (*modelv1.AdminEmergencyCancellation, *modelv1.AdminEmergencyCancellationTask) {
	return &modelv1.AdminEmergencyCancellation{
		Id:                   9007199254740993,
		CreateAt:             1793500000000,
		ActorUserId:          9007199254740995,
		StrategyInstanceId:   9007199254740997,
		Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
		ExpectedRiskRevision: 9007199254741001,
		RiskRevision:         9007199254741002,
	}, &modelv1.AdminEmergencyCancellationTask{
		Id:                      1,
		EmergencyCancellationId: 9007199254740993,
		StrategyInstanceId:      9007199254740997,
		State:                   string(domain.AdminEmergencyCancelAccepted),
		Revision:                9007199254740999,
	}
}
