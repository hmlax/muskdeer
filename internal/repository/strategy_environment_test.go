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

func TestPlanningCapacityKeepsOnlyOwnReusableReservation(t *testing.T) {
	capacity, err := planningCapacity("500.000000000000000000000000000000000001", "400", decimal.NewFromInt(200))
	if err != nil || capacity.String() != "300.000000000000000000000000000000000001" {
		t.Fatalf("allocation lost precision: %s, %v", capacity, err)
	}
	for _, reserved := range []string{"199", "501"} {
		if _, err := planningCapacity("500", reserved, decimal.NewFromInt(200)); !errors.Is(err, domain.ErrRiskDenied) {
			t.Fatalf("inconsistent shared obligation allowed: %v", err)
		}
	}
}

func TestStrategyEnvironmentRejectsIncompleteSpecBeforeDatabase(t *testing.T) {
	reader, err := NewStrategyEnvironment(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := reader.Current(ctx, domain.StrategySpec{}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal(err)
	}
	if _, err := reader.Current(context.Background(), domain.StrategySpec{}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal(err)
	}
}
