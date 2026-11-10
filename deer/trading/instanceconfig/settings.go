package instanceconfig

import (
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// Settings adapts one instance to execution contracts without merging parameters.
func (i *Instance) Settings() (*venueconfig.RuntimeConfig, error) {
	if i == nil || Validate(i.Type, i.Implementation, i.Parameters) != nil || i.Profile == nil {
		return nil, ErrInvalid
	}
	p := i.Parameters
	policy := &commonv1.VenuePolicy{
		Profiles: []*commonv1.VenueProfilePolicy{proto.CloneOf(i.Profile)},
		LeaseTtl: durationpb.New(time.Duration(p.Execution.LeaseTtlMs) * time.Millisecond),
	}
	settings := &venueconfig.RuntimeConfig{
		PollInterval:     time.Duration(p.Execution.PollIntervalMs) * time.Millisecond,
		OperationTimeout: time.Duration(p.Execution.OperationTimeoutMs) * time.Millisecond,
		Venue:            policy,
		InstanceID:       i.ID,
		Revision:         i.Revision,
		ResumeSequence:   i.ResumeSequence,
		Template:         i.Type,
	}
	switch i.Type {
	case domain.StrategyTemplateMarketMaker:
		policy.Depth = p.MarketMaker.Depth
		policy.QuantityFraction = p.MarketMaker.QuantityFraction
		settings.ReplacementFraction = decimal.RequireFromString(p.MarketMaker.ReplacementFraction)
		settings.BridgeSlots = p.MarketMaker.BridgeSlots
	case domain.StrategyTemplateGrid:
		if p.LongGrid != nil {
			policy.Depth = 1
			settings.LongGrid = proto.CloneOf(p.LongGrid)
			order, e1 := domain.ParseLedgerDecimal(i.Profile.MaximumOrderNotional, false, false)
			maximum, e2 := domain.ParseLedgerDecimal(i.Profile.MaximumNotional, false, false)
			requested, e3 := domain.ParseLedgerDecimal(p.LongGrid.OrderNotional, false, false)
			requestedMax, e4 := domain.ParseLedgerDecimal(p.LongGrid.MaximumPositionNotional, false, false)
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil || requested.GreaterThan(order) || requestedMax.GreaterThan(maximum) {
				return nil, ErrInvalid
			}
			break
		}
		policy.Depth = uint32(p.Grid.GridCount)
		settings.Grid = proto.CloneOf(p.Grid)
		settings.ReplacementFraction = decimal.NewFromInt(1)
	case domain.StrategyTemplateWashTrade:
		wash := p.WashTrade
		policy.Depth = 1 // Book mode observes the best bid/ask by definition.
		policy.QuantityFraction = wash.QuantityFraction
		policy.SyntheticPolicyDigest = i.TapePolicyDigest
		mode := configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK
		if wash.Mode == commonv1.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE {
			mode = configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
		}
		policy.SyntheticTape = &configv1.SyntheticTape{
			Enabled:         true,
			DevelopmentOnly: wash.DevelopmentOnly,
			Mode:            mode,
			RunId:           wash.RunId,
			Interval:        durationpb.New(time.Duration(wash.IntervalMs) * time.Millisecond),
			MaximumEvents:   wash.MaximumEvents,
			Profiles: []*configv1.SyntheticTapeProfile{{
				Symbol:          i.Profile.Symbol,
				Quantity:        wash.Quantity,
				MaximumNotional: wash.MaximumNotional,
			}},
		}
	default:
		return nil, ErrInvalid
	}
	return settings, nil
}
