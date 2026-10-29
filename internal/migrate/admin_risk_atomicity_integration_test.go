//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskConcurrentReplayAndCompetingActorsHaveOneEffect(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	other := riskActorFixture(ctx, t, db, "risk-second-operator", domain.AdminRoleOperator, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	request := riskBlockRequest(submit.Input.Authority.StrategyID)
	type outcome struct {
		receipt domain.AdminRiskReceipt
		err     error
	}
	results := make(chan outcome, 6)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			<-start
			receipt, err := control.Change(ctx, actor.Token, actor.CSRF, request)
			results <- outcome{
				receipt: receipt,
				err:     err,
			}
		})
	}
	close(start)
	workers.Wait()
	var first domain.AdminRiskReceipt
	for range 6 {
		result := <-results
		if result.err != nil || result.receipt.ID <= 0 {
			t.Fatal("identical concurrent risk request failed", result.err)
		}
		if first.ID == 0 {
			first = result.receipt
		} else if result.receipt != first {
			t.Fatal("same actor/key produced multiple effects")
		}
	}
	if _, err := control.Change(ctx, other.Token, other.CSRF, request); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("second actor bypassed the current risk revision", err)
	}
	next := request
	next.ExpectedRevision = 2
	start = make(chan struct{})
	for index, identity := range []domain.AdminLogin{actor, other} {
		candidate := next
		candidate.Key = []string{"risk-competing-block-0001", "risk-competing-block-0002"}[index]
		workers.Go(func() {
			<-start
			receipt, err := control.Change(ctx, identity.Token, identity.CSRF, candidate)
			results <- outcome{
				receipt: receipt,
				err:     err,
			}
		})
	}
	close(start)
	workers.Wait()
	winners, conflicts := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil && result.receipt.Revision == 3 && !result.receipt.Enabled:
			winners++
		case errors.Is(result.err, domain.ErrAdminConflict):
			conflicts++
		default:
			t.Fatal("invalid competing risk result", result.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("competing actors did not yield exactly one effect")
	}
	replay, err := control.Change(ctx, actor.Token, actor.CSRF, request)
	if err != nil || replay != first {
		t.Fatal("later risk revision changed historical replay", err)
	}
	request.Reason = "changed payload for the same key"
	if _, err := control.Change(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("key was rebound to different content", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	assertRiskControlState(ctx, t, control, actor, request.StrategyID, false, 3)
}

func TestAdminRiskAuditAndReceiptFailuresRollBackEveryEffect(t *testing.T) {
	for _, receiptFailure := range []bool{false, true} {
		name := "audit"
		if receiptFailure {
			name = "receipt"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			instance := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
			budget := activationBudget(ctx, t, db, instance.Id)
			var risk modelv1.RiskSnapshot
			if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", instance.Id).Take(&risk).Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New("test-only risk write failure")
			const callback = "test:reject-risk-storage"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				_, audit := tx.Statement.Dest.(*modelv1.AdminAudit)
				_, receipt := tx.Statement.Dest.(*modelv1.AdminStrategyRiskChange)
				if audit && !receiptFailure || receipt && receiptFailure {
					tx.Error = injected
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Callback().Create().Remove(callback); err != nil {
					t.Error(err)
				}
			})
			control := riskControlFixture(t, db)
			result, err := control.Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(instance.Id))
			if !errors.Is(err, domain.ErrAdminControlUnavailable) || !errors.Is(err, injected) || result.ID != 0 {
				t.Fatal("failed storage lost its private cause or returned a partial receipt", err)
			}
			var after modelv1.RiskSnapshot
			if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", risk.Id).Take(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(instance, publicationInstance(ctx, t, db, instance.Id)) || !proto.Equal(budget, activationBudget(ctx, t, db, instance.Id)) || !proto.Equal(&risk, &after) {
				t.Fatal("failed audit/receipt partially changed strategy, budget or cached risk")
			}
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
			assertGatewayOrderCount(ctx, t, db, 0)
		})
	}
}

func TestAdminRiskReplayRequiresCurrentAuthorityAndCSRF(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "risk-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	request := riskBlockRequest(submit.Input.Authority.StrategyID)
	assertRiskControlState(ctx, t, control, viewer, request.StrategyID, true, 1)
	if _, err := control.Change(ctx, viewer.Token, viewer.CSRF, request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("viewer changed risk", err)
	}
	if _, err := control.Change(ctx, actor.Token, "invalid-csrf", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("risk control ignored CSRF", err)
	}
	if _, err := control.Change(ctx, actor.Token, actor.CSRF, request); err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Change(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked session replayed risk history", err)
	}
	if _, err := control.State(ctx, actor.Token, request.StrategyID); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked session read risk state", err)
	}
	assertRiskControlState(ctx, t, control, viewer, request.StrategyID, false, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
}
