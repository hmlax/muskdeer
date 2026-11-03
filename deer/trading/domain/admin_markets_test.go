package domain

import (
	"errors"
	"testing"
)

func TestAdminMarketFilterRequiresExplicitModeAndKnownSource(t *testing.T) {
	filter := AdminMarketFilter{
		Page: AdminPage{
			AfterID: 9007199254740993,
			Limit:   MaxAdminPageSize,
		},
		InstrumentID: 9007199254740994,
		Mode:         MarketModeReplay,
	}
	for _, source := range []MarketSource{MarketSourceUnspecified, MarketSourceBinance, MarketSourceOKX} {
		filter.Source = source
		if err := filter.Validate(); err != nil {
			t.Fatal("valid explicit market filter rejected", err)
		}
	}
	for _, change := range []func(*AdminMarketFilter){
		func(f *AdminMarketFilter) { f.Mode = "" },
		func(f *AdminMarketFilter) { f.Mode = "live" },
		func(f *AdminMarketFilter) { f.Source = MarketSourceBitget },
		func(f *AdminMarketFilter) { f.Source = "%' OR true --" },
		func(f *AdminMarketFilter) { f.InstrumentID = -1 },
		func(f *AdminMarketFilter) { f.Page.AfterID = -1 },
		func(f *AdminMarketFilter) { f.Page.Limit = 0 },
		func(f *AdminMarketFilter) { f.Page.Limit = MaxAdminPageSize + 1 },
	} {
		candidate := filter
		change(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatal("unsafe market filter accepted", err)
		}
	}
}
