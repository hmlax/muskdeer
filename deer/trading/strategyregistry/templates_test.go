package strategyregistry

import (
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestTemplateContractsDescribeAllThreeImplementedAlgorithms(t *testing.T) {
	templates := Templates()
	if len(templates) != 4 {
		t.Fatal("missing registered templates")
	}
	for _, template := range templates {
		if !Matches(template) || len(template.ParameterContract.ExecutionFields) != 4 || len(template.ParameterContract.AlgorithmFields) == 0 {
			t.Fatal("template has no complete typed parameter contract")
		}
		altered := proto.CloneOf(template)
		altered.Implementation = "unregistered"
		if Matches(altered) {
			t.Fatal("database template selected code that does not exist")
		}
		altered = proto.CloneOf(template)
		altered.RequiredInputs.OrderBook = false
		if Matches(altered) {
			t.Fatal("template could omit an input required by its algorithm")
		}
	}
	wash := templates[2]
	if wash.RequiredInputs.Account || wash.RequiredInputs.TradesWhenWashMode != common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE {
		t.Fatal("WashTrade dependencies imply financial execution or hidden source fallback")
	}
}
