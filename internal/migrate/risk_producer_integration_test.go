//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestRiskProducerRequiresProjectionAndClipsGrantToInstanceMarketAge(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	denied, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID)
	if err != nil || denied.Allowed || denied.Reconciled || denied.Reason != domain.RiskReasonUnreconciled {
		t.Fatalf("missing projection granted risk: %+v, %v", denied, err)
	}
	portfolioFixture(ctx, t, db, submit)
	feed, token, snapshots, current := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	fresh, err := snapshots.Publish(ctx, token, current.Revision, marketReadingFixture(feed, current.Reading.Sequence+1, "100"))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID)
	if err != nil || !allowed.Allowed || !allowed.Reconciled || allowed.Reason != domain.RiskReasonReady {
		t.Fatalf("reconciled current input rejected: %+v, %v", allowed, err)
	}
	limit := fresh.Reading.EventTime.Add(500 * time.Millisecond)
	if allowed.ExpiresAt.After(limit) || !allowed.ExpiresAt.After(allowed.ObservedAt) {
		t.Fatalf("grant exceeded instance market age: %+v, latest=%v", allowed, limit)
	}
	var row modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !row.AllowNewRisk || !row.Reconciled || row.MarketMaximumAgeMs != 500 || row.GetMarketFeedId() != feed.ID || row.MarketRevision != fresh.Revision || row.ReasonCode != string(domain.RiskReasonReady) {
		t.Fatalf("grant did not persist actual provenance: %v", &row)
	}
}

func TestRiskProducerCannotReopenAfterFillUntilFactsAreReconciled(t *testing.T) {
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
		TradeID:             "risk-producer-fill",
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       accepted.ClientOrderID,
		CounterpartySubject: "external-risk-fixture",
		Price:               submit.Input.Price,
		Quantity:            decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	denied, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID)
	if err != nil || denied.Allowed || denied.Reconciled || denied.Reason != domain.RiskReasonUnreconciled {
		t.Fatalf("unprojected fill granted risk: %+v, %v", denied, err)
	}
	notice, err := eventwire.Fill(ctx, portfolioEventFixture(accepted, fill))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := portfolio.Apply(ctx, notice); err != nil {
		t.Fatal(err)
	}
	feed, token, snapshots, current := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := snapshots.Publish(ctx, token, current.Revision, marketReadingFixture(feed, current.Reading.Sequence+1, "100")); err != nil {
		t.Fatal(err)
	}
	allowed, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID)
	if err != nil || !allowed.Allowed || !allowed.Reconciled {
		t.Fatalf("reconciled facts did not restore eligible grant: %+v, %v", allowed, err)
	}
}

func TestRiskProducerHonorsPersistentBudgetDisableAndCoreMismatch(t *testing.T) {
	for _, fault := range []string{"budget-disabled", "balance-mismatch", "publisher-lost"} {
		t.Run(fault, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			portfolioFixture(ctx, t, db, submit)
			wanted := domain.RiskReasonBudget
			switch fault {
			case "budget-disabled":
				if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", submit.Input.AccountID).UpdateColumn("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			case "balance-mismatch":
				wanted = domain.RiskReasonUnreconciled
				if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", submit.Input.AccountID, "USDT").UpdateColumn("available", "999").Error; err != nil {
					t.Fatal(err)
				}
			case "publisher-lost":
				wanted = domain.RiskReasonMarket
				_, token, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
				expireLeaseFixture(ctx, t, db, token.Key)
			}
			evaluator, err := repository.NewRiskEvaluator(db)
			if err != nil {
				t.Fatal(err)
			}
			denied, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID)
			if err != nil || denied.Allowed || denied.Reason != wanted {
				t.Fatalf("%s re-opened risk: %+v, %v", fault, denied, err)
			}
			var row modelv1.RiskSnapshot
			if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&row).Error; err != nil || row.AllowNewRisk {
				t.Fatalf("denial was not persisted: %v, %v", &row, err)
			}
		})
	}
}
