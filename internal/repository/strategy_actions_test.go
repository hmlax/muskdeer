package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyActionInvalidBoundaryCannotReachStorage(t *testing.T) {
	actions, err := NewStrategyActions(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	authority := domain.StrategyAuthority{
		StrategyID:    1,
		ConfigVersion: 1,
		OwnerID:       "strategy.01234567890123456789012345678901",
		OwnerEpoch:    1,
	}
	valid := domain.StrategyActionPlan{
		Key:          "decision",
		Kind:         domain.StrategyActionSubmit,
		AccountID:    "account",
		InstrumentID: 1,
		Side:         domain.SideBuy,
		Price:        decimal.NewFromInt(99),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
	}
	for _, mutate := range []func(*domain.StrategyActionPlan){
		func(p *domain.StrategyActionPlan) { p.Price = decimal.New(1, 1_000_000_000) },
		func(p *domain.StrategyActionPlan) { p.Quantity = decimal.Zero },
		func(p *domain.StrategyActionPlan) { p.Key = "invalid key" },
		func(p *domain.StrategyActionPlan) { p.TargetIntentID = 1 },
		func(p *domain.StrategyActionPlan) { p.ReduceOnly = true },
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := actions.Plan(ctx, authority, candidate); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatalf("invalid action reached storage: %v", err)
		}
	}
	badOwner := authority
	badOwner.OwnerID = "reusable-name"
	if _, err := actions.Plan(ctx, badOwner, valid); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("unvalidated action owner reached storage")
	}
	cancelPlan := domain.StrategyActionPlan{
		Key:                 "cancel",
		Kind:                domain.StrategyActionCancel,
		AccountID:           "account",
		InstrumentID:        1,
		TargetIntentID:      1,
		TargetClientOrderID: "client",
		Price:               decimal.New(0, 1_000_000_000),
	}
	if _, err := actions.Plan(ctx, authority, cancelPlan); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("extreme zero exponent reached action comparison or storage")
	}
	if _, err := actions.Plan(context.WithoutCancel(ctx), authority, valid); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("unbounded action reached storage")
	}
}
