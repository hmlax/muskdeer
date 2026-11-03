package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestEmergencyCancellationRequiresExplicitStrategyScopeAndRiskConfirmation(t *testing.T) {
	t.Parallel()
	request := AdminEmergencyCancelRequest{
		Key:                  "emergency-cancel-0001",
		Scope:                AdminEmergencyCancelScopeStrategy,
		StrategyID:           9007199254740993,
		ExpectedRiskRevision: 9007199254740995,
		Reason:               "  explicitly close this strategy's risk and cancel orders  ",
	}
	normalized, err := request.Normalize()
	if err != nil || normalized.Reason != strings.TrimSpace(request.Reason) || normalized.StrategyID != request.StrategyID || normalized.ExpectedRiskRevision != request.ExpectedRiskRevision {
		t.Fatal("scope normalization lost confirmation or precision", err)
	}
	for _, change := range []func(*AdminEmergencyCancelRequest){
		func(r *AdminEmergencyCancelRequest) { r.Scope = "" },
		func(r *AdminEmergencyCancelRequest) { r.Scope = "account" },
		func(r *AdminEmergencyCancelRequest) { r.Scope = "all" },
		func(r *AdminEmergencyCancelRequest) { r.StrategyID = 0 },
		func(r *AdminEmergencyCancelRequest) { r.StrategyID = -1 },
		func(r *AdminEmergencyCancelRequest) { r.ExpectedRiskRevision = 0 },
		func(r *AdminEmergencyCancelRequest) { r.ExpectedRiskRevision = math.MaxInt64 },
		func(r *AdminEmergencyCancelRequest) { r.Key = "short" },
		func(r *AdminEmergencyCancelRequest) { r.Key = strings.Repeat("k", 65) },
		func(r *AdminEmergencyCancelRequest) { r.Key = "invalid-key/with-slash" },
		func(r *AdminEmergencyCancelRequest) { r.Reason = " \t\n" },
		func(r *AdminEmergencyCancelRequest) { r.Reason = strings.Repeat("界", 86) },
		func(r *AdminEmergencyCancelRequest) { r.Reason = "unsafe\x00reason" },
		func(r *AdminEmergencyCancelRequest) { r.Reason = string([]byte{0xff}) },
	} {
		candidate := request
		change(&candidate)
		if _, err := candidate.Normalize(); !errors.Is(err, ErrInvalidAdminEmergencyCancel) {
			t.Fatal("invalid or expanded emergency cancellation accepted", err)
		}
	}
}

func TestEmergencyCancellationNeverConflatesEnrollmentOrUnknownSubmissionsWithCompletion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		progress AdminEmergencyCancelProgress
		want     AdminEmergencyCancelState
	}{
		{
			name: "empty scan is not completed enrollment",
			want: AdminEmergencyCancelAccepted,
		},
		{
			name: "completed empty enrollment",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
			},
			want: AdminEmergencyCancelSucceeded,
		},
		{
			name: "unknown submit without OMS identity remains pending",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
				PendingSubmissions: 1,
			},
			want: AdminEmergencyCancelAccepted,
		},
		{
			name: "terminal orders still require completed enrollment",
			progress: AdminEmergencyCancelProgress{
				Targets:  2,
				Terminal: 2,
			},
			want: AdminEmergencyCancelAccepted,
		},
		{
			name: "unknown target keeps accepted even after another failure",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
				Targets:            3,
				Terminal:           1,
				Failed:             1,
			},
			want: AdminEmergencyCancelAccepted,
		},
		{
			name: "all observed with cancellation failure is failed",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
				Targets:            3,
				Terminal:           2,
				Failed:             1,
			},
			want: AdminEmergencyCancelFailed,
		},
		{
			name: "all OMS terminal may include fills and is not flattening",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
				Targets:            3,
				Terminal:           3,
			},
			want: AdminEmergencyCancelSucceeded,
		},
		{
			name: "counter upper bound does not overflow",
			progress: AdminEmergencyCancelProgress{
				EnrollmentComplete: true,
				Targets:            math.MaxInt64,
				Terminal:           math.MaxInt64,
			},
			want: AdminEmergencyCancelSucceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, err := test.progress.State()
			if err != nil || state != test.want {
				t.Fatalf("state %s, want %s: %v", state, test.want, err)
			}
		})
	}
}

func TestEmergencyCancellationRejectsImpossibleCountersBeforeReportingSuccess(t *testing.T) {
	t.Parallel()
	for _, progress := range []AdminEmergencyCancelProgress{
		{
			Targets: -1,
		},
		{
			Terminal: -1,
		},
		{
			Failed: -1,
		},
		{
			PendingSubmissions: -1,
		},
		{
			Targets:  1,
			Terminal: 2,
		},
		{
			Targets: 1,
			Failed:  2,
		},
		{
			Targets:  2,
			Terminal: 1,
			Failed:   2,
		},
		{
			Targets:  math.MaxInt64,
			Terminal: math.MaxInt64,
			Failed:   1,
		},
	} {
		if _, err := progress.State(); !errors.Is(err, ErrInvalidAdminEmergencyCancel) {
			t.Fatal("inconsistent or overflowing aggregate accepted", err)
		}
	}
}
