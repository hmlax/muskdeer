package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAdminOverviewFiltersRequireExactStoredScope(t *testing.T) {
	filter := AdminOverviewFilter{
		Page: AdminPage{
			AfterID: 9007199254740993,
			Limit:   MaxAdminPageSize,
		},
		AccountID: "overview.account:one",
		Source:    MarketSourceOKX,
	}
	if err := filter.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AdminOverviewFilter){
		func(f *AdminOverviewFilter) { f.Page.AfterID = -1 },
		func(f *AdminOverviewFilter) { f.Page.Limit = 0 },
		func(f *AdminOverviewFilter) { f.Page.Limit++ },
		func(f *AdminOverviewFilter) { f.AccountID = "account%" },
		func(f *AdminOverviewFilter) { f.AccountID = strings.Repeat("a", 65) },
		func(f *AdminOverviewFilter) { f.Source = MarketSourceBitget },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("ambiguous or unbounded overview filter accepted", err)
		}
	}
	for _, state := range []StrategyState{StrategyStateCreated, StrategyStateStarting, StrategyStateRecovering, StrategyStateRunning,
		StrategyStatePausing, StrategyStatePaused, StrategyStateStopping, StrategyStateStopped, StrategyStateError} {
		if !ValidStrategyState(state) {
			t.Fatal("defined stored state rejected")
		}
	}
	for _, state := range []StrategyState{"", "running", "HEALTHY"} {
		if ValidStrategyState(state) {
			t.Fatal("undefined state or health grant accepted")
		}
	}
}
