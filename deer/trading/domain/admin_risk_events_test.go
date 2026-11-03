package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAdminRiskEventFilterRequiresExactBoundedHistoryScope(t *testing.T) {
	filter := AdminRiskEventFilter{
		Page: AdminPage{
			AfterID: 9007199254740993,
			Limit:   MaxAdminPageSize,
		},
		StrategyID:   9007199254740994,
		AccountID:    "risk.account:one",
		InstrumentID: 9007199254740995,
		Source:       MarketSourceOKX,
		Kind:         RiskSnapshotEventRemoved,
		Reason:       RiskReasonConfigurationPending,
	}
	if err := filter.Validate(); err != nil {
		t.Fatal("exact high identity history filter rejected", err)
	}
	for _, change := range []func(*AdminRiskEventFilter){
		func(f *AdminRiskEventFilter) { f.Page.AfterID = -1 },
		func(f *AdminRiskEventFilter) { f.Page.Limit = 0 },
		func(f *AdminRiskEventFilter) { f.Page.Limit++ },
		func(f *AdminRiskEventFilter) { f.StrategyID = -1 },
		func(f *AdminRiskEventFilter) { f.InstrumentID = -1 },
		func(f *AdminRiskEventFilter) { f.AccountID = " account" },
		func(f *AdminRiskEventFilter) { f.AccountID = "account%" },
		func(f *AdminRiskEventFilter) { f.AccountID = strings.Repeat("a", 65) },
		func(f *AdminRiskEventFilter) { f.Source = MarketSourceBitget },
		func(f *AdminRiskEventFilter) { f.Kind = "changed" },
		func(f *AdminRiskEventFilter) { f.Reason = "ready" },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("unbounded, wildcard or undefined filter accepted", err)
		}
	}
	if err := (AdminRiskEventFilter{Page: AdminPage{Limit: 1}}).Validate(); err != nil {
		t.Fatal("optional history filters rejected", err)
	}
}

func TestRiskReasonValidationKeepsWildcardOutOfPersistedDiagnostics(t *testing.T) {
	for _, reason := range []RiskReason{RiskReasonNotEvaluated, RiskReasonReady, RiskReasonStopped, RiskReasonParameters,
		RiskReasonBudget, RiskReasonUnreconciled, RiskReasonMarket, RiskReasonConfigurationPending} {
		if !ValidRiskReason(reason) {
			t.Fatal("known persisted reason rejected")
		}
	}
	for _, reason := range []RiskReason{RiskReasonUnspecified, "ready", "UNKNOWN"} {
		if ValidRiskReason(reason) {
			t.Fatal("wildcard or undefined reason became a persisted diagnostic")
		}
	}
}
