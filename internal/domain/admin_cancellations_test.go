package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAdminCancellationRequiresExplicitOrderAndConfirmation(t *testing.T) {
	t.Parallel()
	valid := AdminCancellationRequest{
		Key:                   "cancel-management-0001",
		StrategyID:            9007199254740993,
		OrderID:               9007199254740995,
		ExpectedOrderRevision: 9007199254740997,
		Reason:                "  cancel this order  ",
	}
	normal, err := valid.Normalize()
	if err != nil || normal.Reason != "cancel this order" || normal.OrderID != valid.OrderID || normal.ExpectedOrderRevision != valid.ExpectedOrderRevision {
		t.Fatal("Normalize lost confirmation or canonical reason", err)
	}
	for _, change := range []func(*AdminCancellationRequest){
		func(r *AdminCancellationRequest) { r.StrategyID = 0 },
		func(r *AdminCancellationRequest) { r.OrderID = 0 },
		func(r *AdminCancellationRequest) { r.ExpectedOrderRevision = 0 },
		func(r *AdminCancellationRequest) { r.Key = "short" },
		func(r *AdminCancellationRequest) { r.Key = strings.Repeat("k", 65) },
		func(r *AdminCancellationRequest) { r.Reason = " \t" },
		func(r *AdminCancellationRequest) { r.Reason = strings.Repeat("中", 86) },
		func(r *AdminCancellationRequest) { r.Reason = "bad\x00reason" },
		func(r *AdminCancellationRequest) { r.Reason = string([]byte{0xff}) },
	} {
		candidate := valid
		change(&candidate)
		if _, err := candidate.Normalize(); !errors.Is(err, ErrInvalidAdminCancellation) {
			t.Fatal("Normalize accepted an invalid cancellation", err)
		}
	}
}
