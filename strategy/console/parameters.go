package console

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

const (
	washModeBook      = "book"
	washModeAggregate = "agg_trade"
)

func parametersToWire(p *common.StrategyInstanceParameters) *pb.InstanceParameters {
	if p == nil {
		return nil
	}
	result := &pb.InstanceParameters{}
	if v := p.Execution; v != nil {
		result.Execution = &pb.ExecutionParameters{
			PollIntervalMs:     v.PollIntervalMs,
			OperationTimeoutMs: v.OperationTimeoutMs,
			LeaseTtlMs:         v.LeaseTtlMs,
			Leverage:           v.Leverage,
		}
	}
	switch {
	case p.MarketMaker != nil:
		v := p.MarketMaker
		result.Algorithm = &pb.InstanceParameters_MarketMaker{MarketMaker: &pb.DepthParameters{
			Depth:               v.Depth,
			QuantityFraction:    v.QuantityFraction,
			ReplacementFraction: v.ReplacementFraction,
			BridgeSlots:         v.BridgeSlots,
		}}
	case p.Grid != nil:
		v := p.Grid
		result.Algorithm = &pb.InstanceParameters_Grid{Grid: &pb.GridParameters{
			LowerPrice:       v.LowerPrice,
			UpperPrice:       v.UpperPrice,
			GridCount:        v.GridCount,
			TotalBudget:      v.TotalBudget,
			PerOrderNotional: v.PerOrderNotional,
			InventoryLimit:   v.InventoryLimit,
			MaxMarketAgeMs:   v.MaxMarketAgeMs,
		}}
	case p.WashTrade != nil:
		v := p.WashTrade
		mode := washModeBook
		if v.Mode == common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE {
			mode = washModeAggregate
		}
		result.Algorithm = &pb.InstanceParameters_WashTrade{WashTrade: &pb.WashParameters{
			DevelopmentOnly:  v.DevelopmentOnly,
			Mode:             mode,
			RunId:            v.RunId,
			IntervalMs:       v.IntervalMs,
			MaximumEvents:    v.MaximumEvents,
			Quantity:         v.Quantity,
			MaximumNotional:  v.MaximumNotional,
			QuantityFraction: v.QuantityFraction,
		}}
	}
	return result
}

func parametersFromWire(p *pb.InstanceParameters) (*common.StrategyInstanceParameters, error) {
	if p.GetExecution() == nil {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	e := p.Execution
	result := &common.StrategyInstanceParameters{
		SchemaVersion: instanceconfig.ParametersSchemaVersion,
		Execution: &common.StrategyExecutionParameters{
			PollIntervalMs:     e.PollIntervalMs,
			OperationTimeoutMs: e.OperationTimeoutMs,
			LeaseTtlMs:         e.LeaseTtlMs,
			Leverage:           e.Leverage,
		},
	}
	switch v := p.Algorithm.(type) {
	case *pb.InstanceParameters_MarketMaker:
		if v.MarketMaker == nil {
			return nil, status.Error(codes.InvalidArgument, messageInvalid)
		}
		result.MarketMaker = &common.DepthMakerParameters{
			Depth:               v.MarketMaker.Depth,
			QuantityFraction:    v.MarketMaker.QuantityFraction,
			ReplacementFraction: v.MarketMaker.ReplacementFraction,
			BridgeSlots:         v.MarketMaker.BridgeSlots,
		}
	case *pb.InstanceParameters_Grid:
		if v.Grid == nil {
			return nil, status.Error(codes.InvalidArgument, messageInvalid)
		}
		result.Grid = &common.GridParameters{
			LowerPrice:       v.Grid.LowerPrice,
			UpperPrice:       v.Grid.UpperPrice,
			GridCount:        v.Grid.GridCount,
			TotalBudget:      v.Grid.TotalBudget,
			PerOrderNotional: v.Grid.PerOrderNotional,
			InventoryLimit:   v.Grid.InventoryLimit,
			MaxMarketAgeMs:   v.Grid.MaxMarketAgeMs,
		}
	case *pb.InstanceParameters_WashTrade:
		if v.WashTrade == nil {
			return nil, status.Error(codes.InvalidArgument, messageInvalid)
		}
		var mode common.WashTradeMode
		switch v.WashTrade.Mode {
		case washModeBook:
			mode = common.WashTradeMode_WASH_TRADE_MODE_BOOK
		case washModeAggregate:
			mode = common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE
		default:
			return nil, status.Error(codes.InvalidArgument, messageInvalid)
		}
		result.WashTrade = &common.WashTradeParameters{
			DevelopmentOnly:  v.WashTrade.DevelopmentOnly,
			Mode:             mode,
			RunId:            v.WashTrade.RunId,
			IntervalMs:       v.WashTrade.IntervalMs,
			MaximumEvents:    v.WashTrade.MaximumEvents,
			Quantity:         v.WashTrade.Quantity,
			MaximumNotional:  v.WashTrade.MaximumNotional,
			QuantityFraction: v.WashTrade.QuantityFraction,
		}
	default:
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	return result, nil
}
