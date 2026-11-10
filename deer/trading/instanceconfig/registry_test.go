package instanceconfig

import (
	"testing"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestThreeImplementationsRequireTheirOwnConcreteParameters(t *testing.T) {
	t.Parallel()
	definitions := Definitions()
	if len(definitions) != 4 {
		t.Fatalf("Definitions() = %d entries, want maker/two grid implementations/WashTrade", len(definitions))
	}
	for _, definition := range definitions {
		t.Run(string(definition.Type), func(t *testing.T) {
			t.Parallel()
			p := parameterFixture(definition.Type)
			if definition.Implementation == ImplementationLongGrid {
				p.Grid = nil
				p.LongGrid = &commonv1.LongGridParameters{
					SpacingFraction:         "0.01",
					StopLossFraction:        "0.005",
					OrderNotional:           "100",
					MaximumPositionNotional: "100",
					OpenOnStart:             true,
					HaltAfterStopLoss:       true,
				}
			}
			if err := Validate(definition.Type, definition.Implementation, p); err != nil {
				t.Fatalf("Validate(%s) = %v, want valid complete parameters", definition.Type, err)
			}
			if err := Validate(definition.Type, "unknown", p); err == nil {
				t.Fatal("unknown implementation fell back to a registered algorithm")
			}
			p.SchemaVersion = 0
			if err := Validate(definition.Type, definition.Implementation, p); err == nil {
				t.Fatal("missing parameter schema was accepted")
			}
		})
	}
}

func TestIndependentInstancesDoNotRequireIdenticalControls(t *testing.T) {
	t.Parallel()
	first := parameterFixture(domain.StrategyTemplateMarketMaker)
	second := proto.CloneOf(first)
	second.Execution.PollIntervalMs = 350
	second.MarketMaker.QuantityFraction = "0.2"
	for _, parameters := range []*commonv1.StrategyInstanceParameters{
		first,
		second,
	} {
		if err := Validate(domain.StrategyTemplateMarketMaker, ImplementationMaker, parameters); err != nil {
			t.Fatal(err)
		}
	}
	second.WashTrade = parameterFixture(domain.StrategyTemplateWashTrade).WashTrade
	if err := Validate(domain.StrategyTemplateMarketMaker, ImplementationMaker, second); err == nil {
		t.Fatal("a maker containing a second WashTrade strategy was accepted")
	}
}

func TestInputRequirementsAndParameterIdentityAreExplicit(t *testing.T) {
	t.Parallel()
	p := parameterFixture(domain.StrategyTemplateWashTrade)
	needs := Requirements(domain.StrategyTemplateWashTrade, p)
	if !needs.OrderBook || !needs.Trades || needs.Account {
		t.Fatalf("WashTrade requirements = %+v, want book+trades without financial accounts", needs)
	}
	first, err := ParametersDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	cloned := proto.CloneOf(p)
	same, err := ParametersDigest(cloned)
	if err != nil || same != first {
		t.Fatal("identical typed parameters changed their identity")
	}
	cloned.WashTrade.Quantity = "0.02"
	changed, err := ParametersDigest(cloned)
	if err != nil || changed == first {
		t.Fatal("changed parameters retained a previous run's identity")
	}
}

func parameterFixture(kind domain.StrategyTemplate) *commonv1.StrategyInstanceParameters {
	p := &commonv1.StrategyInstanceParameters{
		SchemaVersion: ParametersSchemaVersion,
		Execution: &commonv1.StrategyExecutionParameters{
			PollIntervalMs:     200,
			OperationTimeoutMs: 2000,
			LeaseTtlMs:         10000,
			Leverage:           10,
		},
	}
	switch kind {
	case domain.StrategyTemplateMarketMaker:
		p.MarketMaker = &commonv1.DepthMakerParameters{
			Depth:               20,
			QuantityFraction:    "0.1",
			ReplacementFraction: "0.1",
			BridgeSlots:         1,
		}
	case domain.StrategyTemplateGrid:
		p.Grid = &commonv1.GridParameters{
			LowerPrice:       "90",
			UpperPrice:       "110",
			GridCount:        5,
			TotalBudget:      "500",
			PerOrderNotional: "100",
			InventoryLimit:   "10",
			MaxMarketAgeMs:   1000,
		}
	case domain.StrategyTemplateWashTrade:
		p.WashTrade = &commonv1.WashTradeParameters{
			DevelopmentOnly:  true,
			Mode:             commonv1.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
			RunId:            "instance-test",
			IntervalMs:       1000,
			MaximumEvents:    100,
			Quantity:         "0.01",
			MaximumNotional:  "100",
			QuantityFraction: "0.1",
		}
	}
	return p
}
