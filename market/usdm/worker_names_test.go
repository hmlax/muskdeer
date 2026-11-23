package usdm

import (
	"regexp"
	"testing"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestCatalogWorkerNamesRemainSafeAndDistinct(t *testing.T) {
	runtime := &Runtime{children: make(map[string]*Runtime)}
	for _, symbol := range []string{"BTCUSDT", "币安人生USDT"} {
		runtime.children[symbol] = &Runtime{
			cfg: &venueconfig.RuntimeConfig{
				Venue: &common.VenuePolicy{
					Profiles: []*common.VenueProfilePolicy{{Symbol: symbol}},
					SyntheticTape: &config.SyntheticTape{
						Enabled:  true,
						Mode:     config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE,
						Profiles: []*config.SyntheticTapeProfile{{Symbol: symbol}},
					},
				},
			},
		}
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	seen := make(map[string]bool)
	workers := runtime.Workers()
	if len(workers) != 4 {
		t.Fatalf("workers=%d; want two books and two trade streams", len(workers))
	}
	for _, worker := range workers {
		if !safe.MatchString(worker.Name) || seen[worker.Name] || worker.Run == nil {
			t.Fatalf("invalid worker registration: %q", worker.Name)
		}
		seen[worker.Name] = true
	}
	if runtime.children["币安人生USDT"].cfg.Venue.Profiles[0].Symbol != "币安人生USDT" {
		t.Fatal("worker naming rewrote the trading identity")
	}
}
