package domain

import (
	"errors"
	"testing"
)

func TestAdminControlNormalizesBoundedReasonAndRejectsAmbiguousIdentity(t *testing.T) {
	valid := AdminControlRequest{
		Key:              "operator-request-0001",
		StrategyID:       1,
		ExpectedRevision: 1,
		Action:           AdminControlStop,
		Reason:           "  explicit operational stop  ",
	}
	value, err := valid.Normalize()
	if err != nil || value.Reason != "explicit operational stop" {
		t.Fatal("reason normalization changed business content", err)
	}
	for _, mutate := range []func(*AdminControlRequest){
		func(v *AdminControlRequest) { v.Key = "short" },
		func(v *AdminControlRequest) { v.Key = "operator request 0001" },
		func(v *AdminControlRequest) { v.StrategyID = 0 },
		func(v *AdminControlRequest) { v.ExpectedRevision = 0 },
		func(v *AdminControlRequest) { v.ExpectedRevision = 1<<63 - 1 },
		func(v *AdminControlRequest) { v.Reason = "\x00invalid" },
		func(v *AdminControlRequest) { v.Reason = "\xff" },
		func(v *AdminControlRequest) { v.Reason = "  " },
		func(v *AdminControlRequest) { v.Action = AdminControlAction("strategy.close_position") },
	} {
		changed := valid
		mutate(&changed)
		if _, err := changed.Normalize(); !errors.Is(err, ErrInvalidAdminControl) {
			t.Fatal("invalid control identity crossed boundary", err)
		}
	}
}
