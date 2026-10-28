//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyRemoteRiskRefusalRetainsRealAttemptWithoutCoreEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	_, execution, market := strategyControllerFixture(ctx, t, db, submit)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	refusal := &refuseOneRemoteSubmission{
		StrategyExecution: execution,
		db:                db,
	}
	controller := strategyControllerWith(ctx, t, db, submit, leases, refusal, market)
	for range 8 {
		strategyControllerTick(ctx, t, controller, market)
		if refusal.refused {
			break
		}
		time.Sleep(110 * time.Millisecond)
	}
	if !refusal.refused || execution.submits != 1 {
		t.Fatal("controlled remote risk refusal did not execute exactly once")
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	active, err := actions.Active(ctx, submit.Input.Authority.StrategyID)
	if err != nil || len(active) != 1 || active[0].State != domain.StrategyActionDispatching {
		t.Fatal("remote refusal erased the durable attempted identity")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	// Only the pre-existing deadline proof can resolve this absent request.
	if remaining := time.Until(active[0].Deadline); remaining > 0 {
		timer := time.NewTimer(remaining + time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-timer.C:
		}
	}
	for range 8 {
		strategyControllerTick(ctx, t, controller, market)
	}
	resolved, err := actions.ByKey(ctx, active[0].StrategyID, active[0].Plan.Key)
	if err != nil || resolved.ID != active[0].ID || resolved.IdempotencyKey != active[0].IdempotencyKey || resolved.State != domain.StrategyActionNotAccepted || execution.submits != 1 {
		t.Fatal("refused submission was resent or resolved without its original deadline proof")
	}
	var instance modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, active[0].StrategyID).Error; err != nil || instance.State != string(domain.StrategyStateStopped) {
		t.Fatal("observed remote refusal prevented terminal stop")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
}

type refuseOneRemoteSubmission struct {
	domain.StrategyExecution
	db      *gorm.DB
	refused bool
}

func (r *refuseOneRemoteSubmission) Submit(ctx context.Context, input domain.OrderInput) (domain.IntentSnapshot, error) {
	if !r.refused {
		if err := r.db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", input.Authority.StrategyID).UpdateColumn("allow_new_risk", false).Error; err != nil {
			return domain.IntentSnapshot{}, err
		}
		r.refused = true
	}
	return r.StrategyExecution.Submit(ctx, input)
}
