package strategyconfig

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestTemplateUnionRejectsLegacySchemaAndUnknownPayload(t *testing.T) {
	input := &commonv1.StrategyParameters{Template: &commonv1.StrategyParameters_MarketMaker{MarketMaker: makerFixture()}}
	if value, err := Decode(domain.ParametersSchemaVersion, input); err != nil || value.Maker == nil || value.Grid != nil || value.Template != domain.StrategyTemplateMarketMaker {
		t.Fatalf("maker union = %v, %v", value, err)
	}
	for _, schema := range []int64{0, 2} {
		if _, err := Decode(schema, input); !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatalf("legacy/future schema %d was activated: %v", schema, err)
		}
	}
	for _, value := range []*commonv1.StrategyParameters{nil, {}, {Template: &commonv1.StrategyParameters_Grid{}}, {Template: (*commonv1.StrategyParameters_Grid)(nil)}} {
		if _, err := Decode(domain.ParametersSchemaVersion, value); !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatalf("empty union was accepted: %v", err)
		}
	}
	grid := &commonv1.StrategyParameters{Template: &commonv1.StrategyParameters_Grid{Grid: gridFixture()}}
	if value, err := Decode(domain.ParametersSchemaVersion, grid); err != nil || value.Grid == nil || value.Maker != nil || value.Template != domain.StrategyTemplateGrid {
		t.Fatalf("grid union = %v, %v", value, err)
	}
	input.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
	if _, err := Decode(domain.ParametersSchemaVersion, input); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatalf("unknown union field was accepted: %v", err)
	}
}

func TestMakerParametersPreserveExactBudgetsAndRejectInvalidCombinations(t *testing.T) {
	input := makerFixture()
	output, err := Maker(input)
	if err != nil || output.SideBudget.String() != "20.000000000000000002" {
		t.Fatalf("valid exact maker parameters = %v, %v", output, err)
	}
	for _, change := range []func(*commonv1.MakerParameters){
		func(value *commonv1.MakerParameters) { value.SideBudget = "20.000000000000000001" },
		func(value *commonv1.MakerParameters) { value.LevelNotional = "1e3" },
		func(value *commonv1.MakerParameters) { value.QuoteLevels = domain.MaxQuoteLevels + 1 },
		func(value *commonv1.MakerParameters) { value.LevelSpacingBps = "0" },
		func(value *commonv1.MakerParameters) { value.BaseSpreadBps = "10000" },
		func(value *commonv1.MakerParameters) { value.InventoryHardLimit = "0.5" },
		func(value *commonv1.MakerParameters) { value.MinRefreshIntervalMs = value.MaxQuoteAgeMs + 1 },
		func(value *commonv1.MakerParameters) { value.MaxMarketAgeMs = 1<<63 - 1 },
		func(value *commonv1.MakerParameters) { value.MaxMarketAgeMs = -1 },
		func(value *commonv1.MakerParameters) { value.HedgeEnabled = true },
		func(value *commonv1.MakerParameters) { value.HedgeThreshold = "0.1" },
		func(value *commonv1.MakerParameters) { value.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1}) },
	} {
		changed := proto.CloneOf(input)
		change(changed)
		if _, err := Maker(changed); !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatalf("invalid maker parameters were accepted: %v", err)
		}
	}
	if _, err := Maker(nil); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatalf("nil maker = %v", err)
	}
	enabled := proto.CloneOf(input)
	enabled.HedgeEnabled = true
	enabled.HedgeThreshold = "0.1"
	enabled.MaxHedgeBacklog = "0.2"
	enabled.HedgeMaxWaitMs = 1000
	if _, err := Maker(enabled); err != nil {
		t.Fatal(err)
	}
	enabled.MaxHedgeBacklog = "0.01"
	if _, err := Maker(enabled); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatalf("hedge backlog below threshold accepted: %v", err)
	}
}

func TestGridParametersRejectInsufficientFundingAndInvalidPriceRanges(t *testing.T) {
	input := gridFixture()
	output, err := Grid(input)
	if err != nil || output.TotalBudget.String() != "30.000000000000000003" {
		t.Fatalf("valid exact grid parameters = %v, %v", output, err)
	}
	for _, change := range []func(*commonv1.GridParameters){
		func(value *commonv1.GridParameters) { value.UpperPrice = value.LowerPrice },
		func(value *commonv1.GridParameters) { value.GridCount = 1 },
		func(value *commonv1.GridParameters) { value.GridCount = domain.MaxGridCount + 1 },
		func(value *commonv1.GridParameters) { value.TotalBudget = "30.000000000000000002" },
		func(value *commonv1.GridParameters) { value.LowerPrice = "0.0000000000000000001" },
		func(value *commonv1.GridParameters) { value.MaxMarketAgeMs = 0 },
		func(value *commonv1.GridParameters) { value.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1}) },
	} {
		changed := proto.CloneOf(input)
		change(changed)
		if _, err := Grid(changed); !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatalf("invalid grid parameters accepted: %v", err)
		}
	}
	if _, err := Grid(nil); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatalf("nil grid = %v", err)
	}
}

func makerFixture() *commonv1.MakerParameters {
	return &commonv1.MakerParameters{
		QuoteLevels:          2,
		LevelSpacingBps:      "2.0000000001",
		BaseSpreadBps:        "10.0000000001",
		LevelNotional:        "10.000000000000000001",
		SideBudget:           "20.000000000000000002",
		InventoryTarget:      "0",
		InventorySoftLimit:   "1",
		InventoryHardLimit:   "2",
		RepriceBps:           "1",
		MinRefreshIntervalMs: 100,
		MaxQuoteAgeMs:        1000,
		MaxMarketAgeMs:       500,
		HedgeEnabled:         false,
		HedgeThreshold:       "0",
		HedgeMaxWaitMs:       0,
		MaxHedgeBacklog:      "0",
	}
}

func gridFixture() *commonv1.GridParameters {
	return &commonv1.GridParameters{
		LowerPrice:       "100",
		UpperPrice:       "200",
		GridCount:        3,
		TotalBudget:      "30.000000000000000003",
		PerOrderNotional: "10.000000000000000001",
		InventoryLimit:   "1",
		MaxMarketAgeMs:   500,
	}
}
