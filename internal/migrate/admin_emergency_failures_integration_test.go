//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminEmergencyKnownCancelFailureDoesNotResolveUnknownSubmissionOrReleaseLiveFunds(t *testing.T) {
	for _, submitted := range []bool{false, true} {
		name := "unresolved original submission"
		if submitted {
			name = "confirmed live original order"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			core, intents := executionFixture(t, db)
			submit.Input.Deadline = time.Now().Add(time.Second)
			var order domain.IntentSnapshot
			var err error
			if submitted {
				order, err = intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
			} else {
				order, err = intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
			}
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(order.Input.Authority.StrategyID))
			if err != nil {
				t.Fatal(err)
			}
			progress := emergencyProgressStore(t, db)
			if _, err := progress.Prepare(ctx, accepted.StrategyID, 0); err != nil {
				t.Fatal(err)
			}
			worker, err := repository.NewStrategyOrderCancellations(db)
			if err != nil {
				t.Fatal(err)
			}
			work, err := worker.Prepare(ctx, submit.Input.Authority, 0)
			if err != nil || work.Action == nil {
				t.Fatal("accepted scope did not reach the existing fenced action path", err)
			}
			actions, err := repository.NewStrategyActions(db)
			if err != nil {
				t.Fatal(err)
			}
			attempted, err := actions.Attempt(ctx, *work.Action, submit.Input.Authority, time.Now().Add(150*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			// No call is sent. Only the original deadline/absence protocol may
			// prove NOT_ACCEPTED; it says nothing about the parent submission.
			waitRiskDatabaseExpiry(ctx, t, db, actor.Principal.UserID, attempted.Deadline.UnixMilli())
			observed, err := actions.Observe(ctx, attempted, submit.Input.Authority)
			if err != nil || observed.State != domain.StrategyActionNotAccepted {
				t.Fatal("cancel absence was not sealed by its real protocol", err)
			}
			if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil {
				t.Fatal(err)
			}
			result, err := progress.Prepare(ctx, accepted.StrategyID, 0)
			if err != nil || result.Progress.Terminal != 0 {
				t.Fatal("failed cancel fabricated terminal order proof", err)
			}
			if submitted {
				if result.State != domain.AdminEmergencyCancelFailed || result.Progress.Failed != 1 || result.CompletedAt.IsZero() {
					t.Fatal("known live target did not retain its explicit cancellation failure")
				}
				assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
			} else if result.State != domain.AdminEmergencyCancelAccepted || result.Progress.Failed != 0 || !result.CompletedAt.IsZero() {
				t.Fatal("unresolved original submission was treated as a resolved failure")
			}
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
			assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
			assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
		})
	}
}
