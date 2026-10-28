package usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyTemplateUnionCannotFallBackToAnotherAlgorithm(t *testing.T) {
	for _, parameters := range []domain.StrategyParameters{
		{
			Template: domain.StrategyTemplateGrid,
			Maker:    &domain.MakerParameters{},
		},
		{
			Template: domain.StrategyTemplateGrid,
			Maker:    &domain.MakerParameters{},
			Grid:     &domain.GridParameters{},
		},
		{
			Template: domain.StrategyTemplateMarketMaker,
			Grid:     &domain.GridParameters{},
		},
		{
			Template: domain.StrategyTemplateMarketMaker,
			Maker:    &domain.MakerParameters{},
			Grid:     &domain.GridParameters{},
		},
		{},
	} {
		_, err := strategyQuoteTargets(domain.StrategySpec{Template: parameters.Template}, domain.StrategyEnvironmentView{Parameters: parameters}, domain.MarketReference{}, time.Now())
		if !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatal("ambiguous template reached an algorithm or silently fell back")
		}
	}
	_, err := strategyQuoteTargets(domain.StrategySpec{Template: domain.StrategyTemplateGrid}, domain.StrategyEnvironmentView{
		Parameters: domain.StrategyParameters{
			Template: domain.StrategyTemplateMarketMaker,
			Maker:    &domain.MakerParameters{},
		},
	}, domain.MarketReference{}, time.Now())
	if !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatal("environment template diverged from its active spec")
	}
}
