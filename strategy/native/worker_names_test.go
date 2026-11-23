package native

import (
	"regexp"
	"testing"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestConfiguredCatalogNamesFitLifecycle(t *testing.T) {
	safe := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	for _, kind := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateWashTrade} {
		cfg := &venueconfig.RuntimeConfig{
			InstanceID: 291,
			Template:   kind,
			Venue: &common.VenuePolicy{
				Profiles: []*common.VenueProfilePolicy{{Symbol: "币安人生USDT"}},
				SyntheticTape: &config.SyntheticTape{
					Enabled:  kind == domain.StrategyTemplateWashTrade,
					Profiles: []*config.SyntheticTapeProfile{{Symbol: "币安人生USDT"}},
				},
			},
		}
		workers, err := configuredWorkers(nil, "test", cfg, nil, nil)
		if err != nil || len(workers) != 1 {
			t.Fatalf("configured workers: %v; count=%d", err, len(workers))
		}
		if !safe.MatchString(workers[0].Name+".9223372036854775807") || workers[0].Run == nil {
			t.Fatalf("unsafe worker registration for %s: %q", kind, workers[0].Name)
		}
		if cfg.Venue.Profiles[0].Symbol != "币安人生USDT" {
			t.Fatal("worker naming rewrote the trading identity")
		}
	}
}
