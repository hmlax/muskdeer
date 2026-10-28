//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestSoakSampleAcceptsClosedRiskDuringActualProjectionLag(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	core, store := executionFixture(t, db)
	service := intentServiceFixture(t, store, core)
	accepted, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	fill, err := core.Fill(ctx, domain.CoreFillRequest{
		TradeID:             "soak-unprojected-fill",
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       accepted.ClientOrderID,
		CounterpartySubject: "external-soak-risk-fixture",
		Price:               submit.Input.Price,
		Quantity:            decimal.RequireFromString("0.01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&risk).Error; err != nil {
		t.Fatal(err)
	}
	if !risk.AllowNewRisk || risk.Reconciled {
		t.Fatal("actual fill did not exercise the retained allow flag with a closed reconciliation gate")
	}
	if sample, err := readSoakSample(ctx, db, submit); err != nil || sample.Reconciled || sample.FillCount != 1 || sample.ProjectedFills != 0 {
		t.Fatalf("legitimate closed-risk projection lag failed the soak invariant: reconciled=%t fills=%d projected=%d err=%v", sample.Reconciled, sample.FillCount, sample.ProjectedFills, err)
	}
	newInput := submit.Input
	newInput.IdempotencyKey, newInput.RequestID = "soak-closed-risk", "soak-closed-risk"
	if _, err := service.Submit(ctx, newInput, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatal("actual admission bypassed reconciliation closure")
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	// A forged current reconciliation flag must still fail the observation.
	refreshBusinessRisk(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", risk.Id).UpdateColumn("reconciled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := readSoakSample(ctx, db, submit); !errors.Is(err, domain.ErrReconciliationMismatch) {
		t.Fatal("an eligible risk snapshot referenced an unprojected fill")
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", risk.Id).UpdateColumn("reconciled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, portfolioEventFixture(accepted, fill))); err != nil {
		t.Fatal(err)
	}
	if sample, err := readSoakSample(ctx, db, submit); err != nil || !sample.Reconciled || sample.FillCount != sample.ProjectedFills {
		t.Fatal("actual projection did not restore a consistent sample")
	}
}
