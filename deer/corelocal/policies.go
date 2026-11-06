package corelocal

import (
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

const localPortfolioConsumer = "portfolio_local"

// Policies returns explicit simulator bootstrap inputs for database maintenance.
// It does not persist or enable accounts, create strategies or alter money.
func Policies(resources Resources) ([]*configv1.RuntimePolicyInput, error) {
	binance, e1 := strconv.ParseInt(resources.BinanceFeedID, 10, 64)
	okx, e2 := strconv.ParseInt(resources.OKXFeedID, 10, 64)
	if e1 != nil || e2 != nil || binance <= 0 || okx <= 0 {
		return nil, ErrResources
	}
	base := func() *configv1.BusinessRuntime {
		return &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(500 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
		}
	}
	market, strategy, execution, portfolio := base(), base(), base(), base()
	market.Market = &configv1.MarketRuntime{
		LeaseTtl: durationpb.New(5 * time.Second),
		Feeds: []*configv1.MarketFeedRuntime{
			localReplay(binance),
			localReplay(okx),
		},
	}
	strategy.Strategy = &configv1.StrategyRuntime{
		AccountIds: []string{AccountID},
		LeaseTtl:   durationpb.New(5 * time.Second),
	}
	execution.ExecutionLeaseTtl = durationpb.New(5 * time.Second)
	execution.Risk = &configv1.RiskRuntime{AccountIds: []string{AccountID}}
	execution.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	portfolio.PortfolioConsumer = localPortfolioConsumer
	return []*configv1.RuntimePolicyInput{
		{
			ServiceName: "market",
			Parameters:  market,
		},
		{
			ServiceName: "strategy",
			Parameters:  strategy,
		},
		{
			ServiceName: "execution",
			Parameters:  execution,
		},
		{
			ServiceName: "portfolio",
			Parameters:  portfolio,
		},
	}, nil
}

func localReplay(feed int64) *configv1.MarketFeedRuntime {
	return &configv1.MarketFeedRuntime{
		FeedId:         feed,
		ReplayBid:      "99.99",
		ReplayAsk:      "100.01",
		ReplayQuantity: "1",
		ReplayInterval: durationpb.New(100 * time.Millisecond),
	}
}
