package domain

import (
	"errors"
	"testing"
)

func TestAdminOrderFiltersRequireExplicitScopeAndBoundedKnownStates(t *testing.T) {
	filter := AdminOrderFilter{
		StrategyID: 9007199254740993,
		Page: AdminPage{
			AfterID: 9007199254740994,
			Limit:   100,
		},
	}
	for _, state := range []OrderState{OrderStateUnspecified, OrderStateIntent, OrderStateSubmitting, OrderStateLive, OrderStatePartiallyFilled, OrderStateUnknown, OrderStateFilled, OrderStateCanceled, OrderStateRejected} {
		filter.State = state
		if err := filter.Validate(); err != nil {
			t.Fatal("valid current-order filter lost its state or int64 precision", err)
		}
	}
	for _, change := range []func(*AdminOrderFilter){
		func(f *AdminOrderFilter) { f.StrategyID = 0 },
		func(f *AdminOrderFilter) { f.Page.AfterID = -1 },
		func(f *AdminOrderFilter) { f.Page.Limit = 0 },
		func(f *AdminOrderFilter) { f.Page.Limit = MaxAdminPageSize + 1 },
		func(f *AdminOrderFilter) { f.State = "%' OR true --" },
		func(f *AdminOrderFilter) { f.OpenOnly = true },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("unsafe or contradictory order filter accepted", err)
		}
	}
	filter.State, filter.OpenOnly = OrderStateUnknown, true
	if err := filter.Validate(); err != nil {
		t.Fatal("unknown obligations were excluded from open orders", err)
	}
}
