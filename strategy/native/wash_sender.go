package native

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// One sequential source worker owns this expiring, in-memory market view.
type washSender struct {
	cfg   *venueconfig.RuntimeConfig
	rules *execution.WashMarketRules
}

func (s *washSender) send(ctx context.Context, client execution.ExecutionClient, input *execution.WashTradeRequest) (*common.SyntheticTapeResult, error) {
	if s.rules == nil || s.rules.ValidUntilMs <= time.Now().UnixMilli() {
		rules, err := client.GetWashMarket(ctx, &execution.WashMarketRequest{
			StrategyInstanceId: s.cfg.InstanceID,
			StrategyRevision:   s.cfg.Revision,
			Symbol:             input.Symbol,
		})
		if err != nil {
			return nil, err
		}
		if rules.GetMarket() == nil || rules.GetValidUntilMs() <= time.Now().UnixMilli() {
			return nil, values.ErrInvalid
		}
		s.rules = rules
	}
	target, err := values.WashTargetForMarket(s.cfg.Venue, input, s.rules.Market)
	if err != nil {
		return nil, err
	}
	outbound := proto.CloneOf(input)
	outbound.Side, outbound.Price, outbound.Quantity = target.Side, target.Price, target.Quantity
	reply, err := client.WashTrade(ctx, outbound)
	if err != nil || reply.GetState() == values.SyntheticSkippedMinimum {
		s.rules = nil
	}
	return reply, err
}
