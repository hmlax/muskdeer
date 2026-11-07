package console

import (
	"testing"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

func TestTypedParametersPreservePrecisionAndUnlimitedZero(t *testing.T) {
	p := &common.StrategyInstanceParameters{
		SchemaVersion: instanceconfig.ParametersSchemaVersion,
		Execution: &common.StrategyExecutionParameters{
			PollIntervalMs:     200,
			OperationTimeoutMs: 2000,
			LeaseTtlMs:         10000,
			Leverage:           10,
		},
		WashTrade: &common.WashTradeParameters{
			DevelopmentOnly:  true,
			Mode:             common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
			RunId:            "console-test",
			IntervalMs:       100,
			MaximumEvents:    0,
			MaximumNotional:  "0",
			Quantity:         "0.000000000000000001",
			QuantityFraction: "0.123456789012345678",
		},
	}
	wire := parametersToWire(p)
	result, err := parametersFromWire(wire)
	if err != nil || !proto.Equal(result, p) {
		t.Fatalf("precision/unlimited values changed: %v %v", result, err)
	}
	result.ManagementChange = &common.StrategyManagementChange{Operation: instanceconfig.ManagementStart}
	if err := instanceconfig.Validate(domain.StrategyTemplateWashTrade, instanceconfig.ImplementationWashTrade, result); err == nil {
		t.Fatal("historical audit accepted as current runtime parameters")
	}
	wire.Algorithm = &pb.InstanceParameters_WashTrade{WashTrade: &pb.WashParameters{Mode: "future-mode"}}
	if _, err := parametersFromWire(wire); err == nil {
		t.Fatal("unknown wash mode silently changed")
	}
}
