//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminPublicationKeepsRiskClosedUntilRequestedPairActivates(t *testing.T) {
	for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
		t.Run(string(source), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
			evaluator, err := repository.NewRiskEvaluator(db)
			if err != nil {
				t.Fatal(err)
			}
			feed, owner, snapshots, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
			refreshLifecycleMarket(ctx, t, feed, owner, snapshots)
			if decision, err := evaluator.Evaluate(ctx, before.Id); err != nil || !decision.Allowed {
				t.Fatal("old selected configuration was not genuinely eligible", err)
			}
			publisher, err := repository.NewAdminStrategyPublisher(db)
			if err != nil {
				t.Fatal(err)
			}
			request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
			request.Source = source
			if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); err != nil {
				t.Fatal(err)
			}
			core, intents := executionFixture(t, db)
			submit.Input.IdempotencyKey = "pending-config-new-risk"
			submit.ClientOrderID = "pending-config-core-risk"
			submit.Input.Quantity = decimal.RequireFromString("0.1")
			submit.Input.Deadline = time.Now().Add(time.Second)
			// Simulate an old writer retaining a fresh, healthy old-version grant.
			// Admission must reject the pair mismatch independently of this flag.
			if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", before.Id).
				Updates(map[string]any{
					"allow_new_risk": true,
					"reason_code":    domain.RiskReasonReady,
					"revision":       gorm.Expr("revision + 1"),
				}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
				t.Fatal("pending configuration admitted a new OMS reservation", err)
			}
			// The final gate also protects direct core commands independently of
			// the cooperative evaluator and OMS adapter.
			if _, err := core.Submit(ctx, submit); !errors.Is(err, domain.ErrRiskDenied) {
				t.Fatal("pending configuration accepted a new final core command", err)
			}
			// Re-evaluating a healthy old source must not revive the old grant.
			refreshLifecycleMarket(ctx, t, feed, owner, snapshots)
			if decision, err := evaluator.Evaluate(ctx, before.Id); err != nil || decision.Allowed || decision.Reason != domain.RiskReasonConfigurationPending {
				t.Fatalf("pending requested input reopened old risk: %+v, %v", decision, err)
			}
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
			assertGatewayOrderCount(ctx, t, db, 0)
			assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
		})
	}
}
