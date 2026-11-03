package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestAdminPublicationBoundsRevisionUnionAndDecimalsBeforeArithmetic(t *testing.T) {
	valid := func() AdminPublishStrategy {
		return AdminPublishStrategy{
			Key:                   "publish-unit-request-0001",
			StrategyID:            1,
			ExpectedRevision:      2,
			ExpectedConfigVersion: 1,
			Source:                MarketSourceOKX,
			Reason:                "  replace requested configuration  ",
			Parameters: StrategyParameters{
				Template: StrategyTemplateGrid,
				Grid: &GridParameters{
					LowerPrice:       decimal.NewFromInt(98),
					UpperPrice:       decimal.NewFromInt(102),
					GridCount:        5,
					TotalBudget:      decimal.NewFromInt(50),
					PerOrderNotional: decimal.NewFromInt(10),
					InventoryLimit:   decimal.NewFromInt(20),
					MaxMarketAge:     time.Second,
				},
			},
		}
	}
	if result, err := valid().Normalize(); err != nil || result.Reason != "replace requested configuration" {
		t.Fatal("valid publication did not normalize its reason", err)
	}
	for _, mutate := range []func(*AdminPublishStrategy){
		func(r *AdminPublishStrategy) { r.StrategyID = 0 },
		func(r *AdminPublishStrategy) { r.ExpectedRevision = 0 },
		func(r *AdminPublishStrategy) { r.ExpectedRevision = math.MaxInt64 },
		func(r *AdminPublishStrategy) { r.ExpectedConfigVersion = 0 },
		func(r *AdminPublishStrategy) { r.ExpectedConfigVersion = math.MaxInt64 },
		func(r *AdminPublishStrategy) { r.Source = MarketSourceBybit },
		func(r *AdminPublishStrategy) { r.Key = "short" },
		func(r *AdminPublishStrategy) { r.Reason = "invalid\x00reason" },
		func(r *AdminPublishStrategy) { r.Reason = strings.Repeat("中", 86) },
		func(r *AdminPublishStrategy) { r.Parameters.Maker = &MakerParameters{} },
		func(r *AdminPublishStrategy) { r.Parameters.Grid = nil },
		func(r *AdminPublishStrategy) { r.Parameters.Grid.TotalBudget = decimal.New(1, 1_000_000_000) },
		func(r *AdminPublishStrategy) { r.Parameters.Grid.PerOrderNotional = decimal.New(1, -1_000_000_000) },
	} {
		request := valid()
		mutate(&request)
		if _, err := request.Normalize(); !errors.Is(err, ErrInvalidAdminPublication) {
			t.Fatal("unsafe publication identity or parameter accepted", err)
		}
	}
}
