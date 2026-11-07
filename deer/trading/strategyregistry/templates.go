// Package strategyregistry identifies compiled algorithms and their typed input
// contracts. Database templates select these implementations; no code is stored
// or executed from an untrusted database string.
package strategyregistry

import (
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Implementation and seed identities remain stable across explicit migrations.
const (
	ParametersSchemaVersion       = 1
	ImplementationMaker           = "depth_maker_v1"
	ImplementationGrid            = "price_grid_v1"
	ImplementationWashTrade       = "wash_trade_v1"
	MakerTemplateID         int64 = 1
	GridTemplateID          int64 = 2
	WashTradeTemplateID     int64 = 3
	makerName                     = "做市铺单"
	gridName                      = "网格"
	washName                      = "WashTrade"
)

// Templates returns private seed records for the three compiled implementations.
func Templates() []*model.StrategyTemplate {
	return []*model.StrategyTemplate{
		definition(MakerTemplateID, domain.StrategyTemplateMarketMaker, ImplementationMaker, makerName, &common.DepthMakerParameters{}, &common.StrategyTemplateInputs{
			OrderBook: true,
			Account:   true,
		}),
		definition(GridTemplateID, domain.StrategyTemplateGrid, ImplementationGrid, gridName, &common.GridParameters{}, &common.StrategyTemplateInputs{
			OrderBook: true,
			Account:   true,
		}),
		definition(WashTradeTemplateID, domain.StrategyTemplateWashTrade, ImplementationWashTrade, washName, &common.WashTradeParameters{}, &common.StrategyTemplateInputs{
			OrderBook:          true,
			TradesWhenWashMode: common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
		}),
	}
}
func definition(id int64, kind domain.StrategyTemplate, implementation, name string, parameters proto.Message, inputs *common.StrategyTemplateInputs) *model.StrategyTemplate {
	execution := (&common.StrategyExecutionParameters{}).ProtoReflect().Descriptor()
	algorithm := parameters.ProtoReflect().Descriptor()
	return &model.StrategyTemplate{
		Id:             id,
		TemplateKey:    implementation,
		Name:           name,
		StrategyType:   string(kind),
		Implementation: implementation,
		Revision:       1,
		Enabled:        true,
		RequiredInputs: inputs,
		ParameterContract: &common.StrategyTemplateContract{
			SchemaVersion:    ParametersSchemaVersion,
			ExecutionMessage: string(execution.FullName()),
			AlgorithmMessage: string(algorithm.FullName()),
			ExecutionFields:  fieldNames(execution),
			AlgorithmFields:  fieldNames(algorithm),
		},
	}
}
func fieldNames(descriptor protoreflect.MessageDescriptor) []string {
	result := make([]string, 0, descriptor.Fields().Len())
	for index := range descriptor.Fields().Len() {
		result = append(result, string(descriptor.Fields().Get(index).Name()))
	}
	return result
}

// Matches verifies that a stored template names code and input contracts this
// binary actually implements. Disabled templates may remain as offline records.
func Matches(row *model.StrategyTemplate) bool {
	if row == nil || row.Id <= 0 || row.Revision <= 0 || row.DeleteAt != nil || row.TemplateKey == "" || row.Name == "" {
		return false
	}
	for _, definition := range Templates() {
		if row.StrategyType == definition.StrategyType && row.Implementation == definition.Implementation && proto.Equal(row.RequiredInputs, definition.RequiredInputs) && proto.Equal(row.ParameterContract, definition.ParameterContract) {
			return true
		}
	}
	return false
}

// TemplateID returns stable migration seed identity, not a runtime fallback.
func TemplateID(kind domain.StrategyTemplate) int64 {
	switch kind {
	case domain.StrategyTemplateMarketMaker:
		return MakerTemplateID
	case domain.StrategyTemplateGrid:
		return GridTemplateID
	case domain.StrategyTemplateWashTrade:
		return WashTradeTemplateID
	default:
		return 0
	}
}
