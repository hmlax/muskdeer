//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminControlReplacementCannotReviveBeforeObserverSeesIntermediateTarget(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, _, viewer, instances := adminStrategiesFixture(ctx, t, db)
	actor, controls := adminControlFixture(ctx, t, db)
	firstRequest := domain.AdminControlRequest{
		Key:              "rapid-control-first-start",
		StrategyID:       instances[0].Id,
		ExpectedRevision: 1,
		Action:           domain.AdminControlStart,
		Reason:           "first explicit target",
	}
	first, err := controls.Control(ctx, actor.Token, actor.CSRF, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "rapid-control-middle-stop",
		StrategyID:       instances[0].Id,
		ExpectedRevision: 2,
		Action:           domain.AdminControlStop,
		Reason:           "replace the first target before observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "rapid-control-final-start",
		StrategyID:       instances[0].Id,
		ExpectedRevision: 3,
		Action:           domain.AdminControlStart,
		Reason:           "a new target cannot revive an earlier receipt",
	})
	if err != nil || latest.State != domain.AdminOperationAccepted || latest.EffectRevision != 4 {
		t.Fatal("latest intent was not separately accepted", err)
	}
	// No observer has run between these committed commands. The intermediate
	// target change must remain a durable fact instead of disappearing.
	for _, operation := range []domain.AdminOperation{first, stop} {
		actual, err := controls.Operation(ctx, viewer.Token, operation.ID)
		if err != nil || actual.State != domain.AdminOperationSuperseded || actual.Revision != 2 || actual.Observation == nil || actual.Observation.StrategyRevision != operation.EffectRevision+1 {
			t.Fatal("an overwritten operation revived before asynchronous observation", err)
		}
	}
	replayed, err := controls.Control(ctx, actor.Token, actor.CSRF, firstRequest)
	if err != nil || replayed.ID != first.ID || replayed.State != domain.AdminOperationSuperseded {
		t.Fatal("replaying an original key revived a replaced command", err)
	}
	assertControlInstance(ctx, t, db, instances[0].Id, domain.StrategyStateCreated, domain.StrategyStateRunning, 4)
	assertFailedControlReplacementRetainsPending(ctx, t, db, controls, actor, latest)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 3)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertFailedControlReplacementRetainsPending(ctx context.Context, t *testing.T, db *gorm.DB, controls domain.AdminStrategyControl, actor domain.AdminLogin, pending domain.AdminOperation) {
	t.Helper()
	const callback = "test:replacement-audit-failure"
	failure := errors.New("controlled replacement audit failure")
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, audit := tx.Statement.Dest.(*modelv1.AdminAudit); audit {
			tx.Error = failure
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	_, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "rapid-control-rollback-stop",
		StrategyID:       pending.StrategyID,
		ExpectedRevision: pending.EffectRevision,
		Action:           domain.AdminControlStop,
		Reason:           "audit failure must roll back supersession too",
	})
	if !errors.Is(err, failure) {
		t.Fatal("replacement did not reach the injected audit failure", err)
	}
	actual, err := controls.Operation(ctx, actor.Token, pending.ID)
	if err != nil || actual.State != domain.AdminOperationAccepted || actual.Observation != nil {
		t.Fatal("failed replacement committed a false superseded result", err)
	}
	assertControlInstance(ctx, t, db, pending.StrategyID, domain.StrategyStateCreated, domain.StrategyStateRunning, pending.EffectRevision)
}
