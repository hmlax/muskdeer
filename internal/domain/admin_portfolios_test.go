package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAdminPortfolioFilterRequiresBoundedExactAccountScope(t *testing.T) {
	filter := AdminPortfolioFilter{
		Page: AdminPage{
			AfterID: 9007199254740993,
			Limit:   MaxAdminPageSize,
		},
		AccountID:    "portfolio.account:one",
		InstrumentID: 9007199254740994,
	}
	if err := filter.Validate(); err != nil {
		t.Fatal("valid high identity or business account rejected", err)
	}
	for _, change := range []func(*AdminPortfolioFilter){
		func(f *AdminPortfolioFilter) { f.Page.AfterID = -1 },
		func(f *AdminPortfolioFilter) { f.Page.Limit = 0 },
		func(f *AdminPortfolioFilter) { f.Page.Limit++ },
		func(f *AdminPortfolioFilter) { f.InstrumentID = -1 },
		func(f *AdminPortfolioFilter) { f.AccountID = " account" },
		func(f *AdminPortfolioFilter) { f.AccountID = "account%" },
		func(f *AdminPortfolioFilter) { f.AccountID = strings.Repeat("a", 65) },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("ambiguous or unbounded portfolio filter accepted", err)
		}
	}
	filter.AccountID, filter.InstrumentID = "", 0
	if err := filter.Validate(); err != nil {
		t.Fatal("explicitly optional filters rejected", err)
	}
}
