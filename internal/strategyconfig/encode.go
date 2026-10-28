package strategyconfig

import (
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Encode writes concrete decimal strings without JSON bridges or floating point.
// The shared decoder also verifies wire precision and whole-millisecond bounds.
func Encode(input domain.StrategyParameters) (*commonv1.StrategyParameters, error) {
	var result *commonv1.StrategyParameters
	switch input.Template {
	case domain.StrategyTemplateMarketMaker:
		if input.Maker == nil || input.Grid != nil {
			return nil, domain.ErrInvalidParameters
		}
		p := input.Maker
		if err := p.Validate(); err != nil {
			return nil, err
		}
		result = &commonv1.StrategyParameters{Template: &commonv1.StrategyParameters_MarketMaker{MarketMaker: &commonv1.MakerParameters{
			QuoteLevels:          p.QuoteLevels,
			LevelSpacingBps:      p.LevelSpacingBPS.String(),
			BaseSpreadBps:        p.BaseSpreadBPS.String(),
			LevelNotional:        p.LevelNotional.String(),
			SideBudget:           p.SideBudget.String(),
			InventoryTarget:      p.InventoryTarget.String(),
			InventorySoftLimit:   p.InventorySoftLimit.String(),
			InventoryHardLimit:   p.InventoryHardLimit.String(),
			RepriceBps:           p.RepriceBPS.String(),
			MinRefreshIntervalMs: p.MinRefreshInterval.Milliseconds(),
			MaxQuoteAgeMs:        p.MaxQuoteAge.Milliseconds(),
			MaxMarketAgeMs:       p.MaxMarketAge.Milliseconds(),
			HedgeEnabled:         p.HedgeEnabled,
			HedgeThreshold:       p.HedgeThreshold.String(),
			HedgeMaxWaitMs:       p.HedgeMaxWait.Milliseconds(),
			MaxHedgeBacklog:      p.MaxHedgeBacklog.String(),
		}}}
	case domain.StrategyTemplateGrid:
		if input.Grid == nil || input.Maker != nil {
			return nil, domain.ErrInvalidParameters
		}
		p := input.Grid
		if err := p.Validate(); err != nil {
			return nil, err
		}
		result = &commonv1.StrategyParameters{Template: &commonv1.StrategyParameters_Grid{Grid: &commonv1.GridParameters{
			LowerPrice:       p.LowerPrice.String(),
			UpperPrice:       p.UpperPrice.String(),
			GridCount:        p.GridCount,
			TotalBudget:      p.TotalBudget.String(),
			PerOrderNotional: p.PerOrderNotional.String(),
			InventoryLimit:   p.InventoryLimit.String(),
			MaxMarketAgeMs:   p.MaxMarketAge.Milliseconds(),
		}}}
	default:
		return nil, domain.ErrInvalidParameters
	}
	if _, err := Decode(domain.ParametersSchemaVersion, result); err != nil {
		return nil, err
	}
	return result, nil
}
