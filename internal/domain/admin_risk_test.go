package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestAdminRiskRequiresExplicitActionAndIndependentPositiveRevision(t *testing.T) {
	request := AdminRiskRequest{
		Key:              "risk-control-request-0001",
		StrategyID:       9007199254740993,
		ExpectedRevision: 9007199254740994,
		Action:           AdminRiskBlock,
		Reason:           "  explicit strategy risk block  ",
	}
	normalized, err := request.Normalize()
	if err != nil || normalized.Reason != "explicit strategy risk block" || normalized.ExpectedRevision != request.ExpectedRevision {
		t.Fatal("risk request changed revision precision or reason normalization", err)
	}
	for _, change := range []func(*AdminRiskRequest){
		func(r *AdminRiskRequest) { r.StrategyID = 0 },
		func(r *AdminRiskRequest) { r.ExpectedRevision = 0 },
		func(r *AdminRiskRequest) { r.ExpectedRevision = math.MaxInt64 },
		func(r *AdminRiskRequest) { r.Key = "short" },
		func(r *AdminRiskRequest) { r.Action = "" },
		func(r *AdminRiskRequest) { r.Action = "unknown" },
		func(r *AdminRiskRequest) { r.ExpectedConfigVersion = 1 },
		func(r *AdminRiskRequest) { r.ExpectedDesiredState = StrategyStateStopped },
		func(r *AdminRiskRequest) { r.Reason = strings.Repeat("界", 86) },
		func(r *AdminRiskRequest) { r.Reason = "null\x00reason" },
		func(r *AdminRiskRequest) { r.Reason = string([]byte{0xff}) },
	} {
		candidate := request
		change(&candidate)
		if _, err := candidate.Normalize(); !errors.Is(err, ErrInvalidAdminRisk) {
			t.Fatal("invalid risk request was accepted", err)
		}
	}
	request.Action = AdminRiskUnblock
	if _, err := request.Normalize(); !errors.Is(err, ErrInvalidAdminRisk) {
		t.Fatal("unblock accepted without binding its displayed configuration and target", err)
	}
	request.ExpectedConfigVersion = 1
	request.ExpectedDesiredState = StrategyStateStopped
	if _, err := request.Normalize(); err != nil {
		t.Fatal("explicit unblock identifier was rejected", err)
	}
	request.ExpectedDesiredState = StrategyStateRunning
	if _, err := request.Normalize(); err != nil {
		t.Fatal("running recovery target was rejected", err)
	}
	request.ExpectedDesiredState = StrategyStateCreated
	if _, err := request.Normalize(); !errors.Is(err, ErrInvalidAdminRisk) {
		t.Fatal("actual state was accepted as a requested target", err)
	}
}
