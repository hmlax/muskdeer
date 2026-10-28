package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestAdminCreationRejectsUnsafeIdentityUnionAndExponentBeforeComputation(t *testing.T) {
	valid := func() AdminCreateStrategy {
		return AdminCreateStrategy{
			Key:          "strategy-create-unit-0001",
			InstanceKey:  "unit-grid",
			AccountID:    "unit-account",
			InstrumentID: 1,
			CodeVersion:  "unit-version",
			Source:       MarketSourceOKX,
			Mode:         MarketModeReplay,
			Reason:       "  explicit grid definition  ",
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
	request, err := valid().Normalize()
	if err != nil || request.Reason != "explicit grid definition" || request.Source != MarketSourceOKX {
		t.Fatal("valid explicit creation did not normalize only its reason", err)
	}
	for _, mutate := range []func(*AdminCreateStrategy){
		func(r *AdminCreateStrategy) { r.Key = "short" },
		func(r *AdminCreateStrategy) { r.InstanceKey = "" },
		func(r *AdminCreateStrategy) { r.AccountID = "account/other" },
		func(r *AdminCreateStrategy) { r.CodeVersion = strings.Repeat("x", 65) },
		func(r *AdminCreateStrategy) { r.InstrumentID = 0 },
		func(r *AdminCreateStrategy) { r.Source = MarketSourceBybit },
		func(r *AdminCreateStrategy) { r.Mode = "" },
		func(r *AdminCreateStrategy) { r.Reason = " " },
		func(r *AdminCreateStrategy) { r.Reason = "invalid\x00reason" },
		func(r *AdminCreateStrategy) { r.Reason = strings.Repeat("中", 86) },
		func(r *AdminCreateStrategy) { r.Parameters.Maker = &MakerParameters{} },
		func(r *AdminCreateStrategy) { r.Parameters.Grid = nil },
		func(r *AdminCreateStrategy) { r.Parameters.Grid.LowerPrice = decimal.New(1, 1_000_000_000) },
		func(r *AdminCreateStrategy) { r.Parameters.Grid.UpperPrice = decimal.New(1, -1_000_000_000) },
	} {
		candidate := valid()
		mutate(&candidate)
		if _, err := candidate.Normalize(); !errors.Is(err, ErrInvalidAdminCreation) {
			t.Fatal("invalid creation identity/union/exponent accepted", err)
		}
	}
}
