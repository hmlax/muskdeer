//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyFillConfirmationHasItsOwnBoundedRequestBudget(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	service := intentServiceFixture(t, store, core)
	if _, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	// Two independently bounded venue calls each take 600ms. A reused one-second
	// parent budget deterministically expires during the duplicate observation.
	slow := &slowStrategyFillCore{
		Simulator: core,
		delay:     600 * time.Millisecond,
	}
	if !fillStrategySide(ctx, t, db, slow, submit, domain.SideBuy) || slow.calls != 2 {
		t.Fatal("actual fill and duplicate observation did not finish")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("independent confirmation repeated the actual fill effect")
	}
}

type slowStrategyFillCore struct {
	domain.Simulator
	delay time.Duration
	calls int
}

func (s *slowStrategyFillCore) Fill(ctx context.Context, request domain.CoreFillRequest) (domain.CoreFill, error) {
	s.calls++
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return domain.CoreFill{}, ctx.Err()
	case <-timer.C:
		return s.Simulator.Fill(ctx, request)
	}
}
