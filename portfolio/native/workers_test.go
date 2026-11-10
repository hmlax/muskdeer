package native

import (
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestSameSymbolFundsHaveIndependentWorkers(t *testing.T) {
	t.Parallel()
	runtime := &Runtime{children: make(map[string]*Runtime)}
	for _, fund := range []int64{10000, 20000} {
		profile := &common.VenueProfilePolicy{
			UserId: fund,
			FundId: fund,
			Symbol: "BTCUSDT",
		}
		runtime.children[values.ScopeKey(fund, fund)] = &Runtime{
			cfg: &venueconfig.RuntimeConfig{Venue: &common.VenuePolicy{Profiles: []*common.VenueProfilePolicy{profile}}},
		}
	}
	workers := runtime.Workers()
	if len(workers) != 2 {
		t.Fatalf("Workers(two funds, same symbol) count = %d, want 2", len(workers))
	}
	names := make(map[string]struct{})
	for _, worker := range workers {
		if _, duplicate := names[worker.Name]; duplicate {
			t.Fatalf("Workers(two funds, same symbol) duplicate name %q", worker.Name)
		}
		if worker.Run == nil {
			t.Fatal("Workers(two funds, same symbol) missing run function")
		}
		names[worker.Name] = struct{}{}
	}
}
