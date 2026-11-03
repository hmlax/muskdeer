package domain

import (
	"errors"
	"testing"
)

func TestAdminTradeFiltersRequireScopeAndExactOptionalOrderAndSide(t *testing.T) {
	filter := AdminTradeFilter{
		StrategyID: 9007199254740993,
		Page: AdminPage{
			AfterID: 9007199254740994,
			Limit:   MaxAdminPageSize,
		},
		OrderID: 9007199254740995,
	}
	for _, side := range []Side{SideUnspecified, SideBuy, SideSell} {
		filter.Side = side
		if err := filter.Validate(); err != nil {
			t.Fatal("valid precise trade scope rejected", err)
		}
	}
	for _, change := range []func(*AdminTradeFilter){
		func(f *AdminTradeFilter) { f.StrategyID = 0 },
		func(f *AdminTradeFilter) { f.OrderID = -1 },
		func(f *AdminTradeFilter) { f.Page.AfterID = -1 },
		func(f *AdminTradeFilter) { f.Page.Limit = 0 },
		func(f *AdminTradeFilter) { f.Page.Limit++ },
		func(f *AdminTradeFilter) { f.Side = "buy" },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("unsafe trade query accepted", err)
		}
	}
}
