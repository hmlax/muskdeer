package business

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestMarketBusinessRequiresExplicitFeedsAndModesWithoutUnusedBroker(t *testing.T) {
	fixture := &configv1.ServiceConfig{
		Name:            "market",
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		GrpcSecurity:    &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}},
		Postgres:        &configv1.Postgres{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(20 * time.Millisecond),
			OperationTimeout: durationpb.New(200 * time.Millisecond),
			Market: &configv1.MarketRuntime{
				LeaseTtl: durationpb.New(3 * time.Second),
				Feeds: []*configv1.MarketFeedRuntime{{
					FeedId:         1,
					ReplayBid:      "99.99",
					ReplayAsk:      "100.01",
					ReplayQuantity: "1",
					ReplayInterval: durationpb.New(100 * time.Millisecond),
				}},
			},
		},
	}
	if err := ValidateConfig(fixture); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds = nil },
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds[0].FeedId = 0 },
		func(c *configv1.ServiceConfig) {
			c.Business.Market.Feeds = append(c.Business.Market.Feeds, proto.CloneOf(c.Business.Market.Feeds[0]))
		},
		func(c *configv1.ServiceConfig) { c.Business.Market.LeaseTtl = durationpb.New(300 * time.Millisecond) },
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds[0].ReplayAsk = "1e2" },
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds[0].ReplayAsk = "99" },
		func(c *configv1.ServiceConfig) { c.Business.Market.Feeds[0].ReplayQuantity = "0" },
		func(c *configv1.ServiceConfig) { c.Business.PortfolioConsumer = "unexpected" },
	} {
		changed := proto.CloneOf(fixture)
		mutate(changed)
		if err := ValidateConfig(changed); err == nil {
			t.Fatal("unsafe market configuration was accepted")
		}
	}
	live := proto.CloneOf(fixture)
	live.Business.Market.Feeds[0] = &configv1.MarketFeedRuntime{FeedId: 1}
	if err := ValidateConfig(live); err != nil {
		t.Fatalf("live binding selection without replay fields rejected: %v", err)
	}
}
