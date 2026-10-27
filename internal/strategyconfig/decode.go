// Package strategyconfig maps explicit Proto parameter boundaries to domain
// decimal values. It has no HTTP, database or global configuration state.
package strategyconfig

import (
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type decimalField struct {
	name   string
	value  string
	target *decimal.Decimal
	bps    bool
}

// Decode rejects legacy/unknown schema versions and missing/unknown template
// payloads before exposing business parameters for activation or computation.
func Decode(schemaVersion int64, input *commonv1.StrategyParameters) (domain.StrategyParameters, error) {
	var output domain.StrategyParameters
	if schemaVersion != domain.ParametersSchemaVersion || !known(input) || input == nil {
		return output, &domain.ParameterError{Field: "parameters_schema_version"}
	}
	switch value := input.Template.(type) {
	case *commonv1.StrategyParameters_MarketMaker:
		if value == nil {
			return output, &domain.ParameterError{Field: "market_maker"}
		}
		parameters, err := Maker(value.MarketMaker)
		if err != nil {
			return output, err
		}
		return domain.StrategyParameters{
			Template: domain.StrategyTemplateMarketMaker,
			Maker:    &parameters,
		}, nil
	case *commonv1.StrategyParameters_Grid:
		if value == nil {
			return output, &domain.ParameterError{Field: "grid"}
		}
		parameters, err := Grid(value.Grid)
		if err != nil {
			return output, err
		}
		return domain.StrategyParameters{
			Template: domain.StrategyTemplateGrid,
			Grid:     &parameters,
		}, nil
	default:
		return output, &domain.ParameterError{Field: "template"}
	}
}

// Maker converts bounded decimal strings and whole millisecond durations.
func Maker(input *commonv1.MakerParameters) (domain.MakerParameters, error) {
	var output domain.MakerParameters
	if !known(input) || input == nil {
		return output, &domain.ParameterError{Field: "market_maker"}
	}
	output.QuoteLevels = input.QuoteLevels
	output.HedgeEnabled = input.HedgeEnabled
	fields := []decimalField{
		{
			name:   "level_spacing_bps",
			value:  input.LevelSpacingBps,
			target: &output.LevelSpacingBPS,
			bps:    true,
		},
		{
			name:   "base_spread_bps",
			value:  input.BaseSpreadBps,
			target: &output.BaseSpreadBPS,
			bps:    true,
		},
		{
			name:   "level_notional",
			value:  input.LevelNotional,
			target: &output.LevelNotional,
		},
		{
			name:   "side_budget",
			value:  input.SideBudget,
			target: &output.SideBudget,
		},
		{
			name:   "inventory_target",
			value:  input.InventoryTarget,
			target: &output.InventoryTarget,
		},
		{
			name:   "inventory_soft_limit",
			value:  input.InventorySoftLimit,
			target: &output.InventorySoftLimit,
		},
		{
			name:   "inventory_hard_limit",
			value:  input.InventoryHardLimit,
			target: &output.InventoryHardLimit,
		},
		{
			name:   "reprice_bps",
			value:  input.RepriceBps,
			target: &output.RepriceBPS,
			bps:    true,
		},
		{
			name:   "hedge_threshold",
			value:  input.HedgeThreshold,
			target: &output.HedgeThreshold,
		},
		{
			name:   "max_hedge_backlog",
			value:  input.MaxHedgeBacklog,
			target: &output.MaxHedgeBacklog,
		},
	}
	if err := parseFields(fields); err != nil {
		return output, err
	}
	for _, field := range []struct {
		name   string
		value  int64
		target *time.Duration
	}{
		{
			name:   "min_refresh_interval_ms",
			value:  input.MinRefreshIntervalMs,
			target: &output.MinRefreshInterval,
		},
		{
			name:   "max_quote_age_ms",
			value:  input.MaxQuoteAgeMs,
			target: &output.MaxQuoteAge,
		},
		{
			name:   "max_market_age_ms",
			value:  input.MaxMarketAgeMs,
			target: &output.MaxMarketAge,
		},
		{
			name:   "hedge_max_wait_ms",
			value:  input.HedgeMaxWaitMs,
			target: &output.HedgeMaxWait,
		},
	} {
		value, err := interval(field.value, field.name)
		if err != nil {
			return output, err
		}
		*field.target = value
	}
	return output, output.Validate()
}

// Grid converts the concrete grid contract without generic object conversions.
func Grid(input *commonv1.GridParameters) (domain.GridParameters, error) {
	var output domain.GridParameters
	if !known(input) || input == nil {
		return output, &domain.ParameterError{Field: "grid"}
	}
	output.GridCount = input.GridCount
	if err := parseFields([]decimalField{
		{
			name:   "lower_price",
			value:  input.LowerPrice,
			target: &output.LowerPrice,
		},
		{
			name:   "upper_price",
			value:  input.UpperPrice,
			target: &output.UpperPrice,
		},
		{
			name:   "total_budget",
			value:  input.TotalBudget,
			target: &output.TotalBudget,
		},
		{
			name:   "per_order_notional",
			value:  input.PerOrderNotional,
			target: &output.PerOrderNotional,
		},
		{
			name:   "inventory_limit",
			value:  input.InventoryLimit,
			target: &output.InventoryLimit,
		},
	}); err != nil {
		return output, err
	}
	var err error
	output.MaxMarketAge, err = interval(input.MaxMarketAgeMs, "max_market_age_ms")
	if err != nil {
		return output, err
	}
	return output, output.Validate()
}

func parseFields(fields []decimalField) error {
	for _, field := range fields {
		rule := domain.DecimalRule{
			IntegerDigits: 20,
			Scale:         18,
			AllowZero:     true,
		}
		if field.bps {
			rule.IntegerDigits = 5
			rule.Scale = 10
		}
		value, err := domain.ParseDecimal(field.value, rule)
		if err != nil {
			return &domain.ParameterError{Field: field.name}
		}
		*field.target = value
	}
	return nil
}

func interval(milliseconds int64, name string) (time.Duration, error) {
	if milliseconds < 0 || milliseconds > domain.MaxStrategyInterval.Milliseconds() {
		return 0, &domain.ParameterError{Field: name}
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func known(value proto.Message) bool {
	return value != nil && value.ProtoReflect().IsValid() && len(value.ProtoReflect().GetUnknown()) == 0
}
