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

func TestAdminControlPauseResumeAndStopCompleteOnlyOnRealRuntimeProof(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor, controls := adminControlFixture(ctx, t, db)
	controller, execution, market, submit := gridControllerFixture(ctx, t, db)
	observer, err := repository.NewAdminOperationObserver(db)
	if err != nil {
		t.Fatal(err)
	}
	orders := waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	var current modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	pause, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "runtime-control-pause-0001",
		StrategyID:       current.Id,
		ExpectedRevision: current.Revision,
		Action:           domain.AdminControlPause,
		Reason:           "operator pause with real open orders",
	})
	if err != nil || pause.State != domain.AdminOperationAccepted {
		t.Fatal("pause intent was not durably accepted", err)
	}
	runGridController(ctx, t, controller, market, "100", 12)
	if completed, err := observer.Observe(ctx, pause.ID); err != nil || completed {
		t.Fatal("pending core cancellation was mistaken for completed pause", err)
	}
	assertAdminOperationCannotComplete(ctx, t, db, pause.ID, current.Id)
	for _, order := range orders {
		if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	runGridController(ctx, t, controller, market, "100", 8)
	assertAdminOperationCompleted(ctx, t, db, controls, observer, actor.Token, pause.ID)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", current.Id).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	resume, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "runtime-control-resume-0001",
		StrategyID:       current.Id,
		ExpectedRevision: current.Revision,
		Action:           domain.AdminControlResume,
		Reason:           "resume the same configured source",
	})
	if err != nil {
		t.Fatal(err)
	}
	orders = waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	assertAdminOperationCompleted(ctx, t, db, controls, observer, actor.Token, resume.ID)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", current.Id).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	stop, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "runtime-control-stop-0001",
		StrategyID:       current.Id,
		ExpectedRevision: current.Revision,
		Action:           domain.AdminControlStop,
		Reason:           "stop and confirm all original core orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	stopGridController(ctx, t, db, controller, execution, market, submit, orders)
	assertAdminOperationCompleted(ctx, t, db, controls, observer, actor.Token, stop.ID)
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
}
