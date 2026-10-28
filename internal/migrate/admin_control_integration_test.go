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

func TestAdminControlReceiptsReplayAndDoNotInventRuntimeCompletion(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, viewer, instances := adminStrategiesFixture(ctx, t, db)
	actor, controls := adminControlFixture(ctx, t, db)
	request := domain.AdminControlRequest{
		Key:              "management-start-0001",
		StrategyID:       instances[0].Id,
		ExpectedRevision: 1,
		Action:           domain.AdminControlStart,
		Reason:           "explicit operator start",
	}
	accepted, err := controls.Control(ctx, actor.Token, actor.CSRF, request)
	if err != nil || accepted.ID <= 0 || accepted.State != domain.AdminOperationAccepted || !accepted.CompletedAt.IsZero() || accepted.EffectRevision != 2 {
		t.Fatal("request acceptance was lost or fabricated as runtime completion", err)
	}
	again, err := controls.Control(ctx, actor.Token, actor.CSRF, request)
	if err != nil || again.ID != accepted.ID || again.EffectRevision != accepted.EffectRevision {
		t.Fatal("same actor/key caused another control effect", err)
	}
	assertAdminControlRejections(ctx, t, controls, actor, viewer, request)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 1)
	assertControlInstance(ctx, t, db, request.StrategyID, domain.StrategyStateCreated, domain.StrategyStateRunning, 2)
	assertAdminOperationCannotComplete(ctx, t, db, accepted.ID, request.StrategyID)
	observer, err := repository.NewAdminOperationObserver(db)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := observer.Observe(ctx, accepted.ID); err != nil || changed {
		t.Fatal("observer invented a running owner", err)
	}
	stop, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "management-stop-0001",
		StrategyID:       request.StrategyID,
		ExpectedRevision: 2,
		Action:           domain.AdminControlStop,
		Reason:           "replace outstanding start target",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := observer.Observe(ctx, accepted.ID); err != nil || !changed {
		t.Fatal("replaced target did not become durably superseded", err)
	}
	old, err := controls.Operation(ctx, viewer.Token, accepted.ID)
	if err != nil || old.State != domain.AdminOperationSuperseded || old.CompletedAt.IsZero() {
		t.Fatal("viewer could not read the durable superseded result", err)
	}
	if changed, err := observer.Observe(ctx, stop.ID); err != nil || changed {
		t.Fatal("requested stop was claimed complete while actual state remained CREATED", err)
	}
	// A legacy epoch-zero stopped record has no runtime or monetary effects.
	// Only the observer changes the result, after observing this real DB state.
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", request.StrategyID).
		Select("State", "Revision").Updates(&modelv1.StrategyInstance{
		State:    string(domain.StrategyStateStopped),
		Revision: 4,
	}).Error; err != nil {
		t.Fatal(err)
	}
	assertAdminOperationCompleted(ctx, t, db, controls, observer, actor.Token, stop.ID)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 0)
}

func assertAdminControlRejections(ctx context.Context, t *testing.T, controls domain.AdminStrategyControl, actor, viewer domain.AdminLogin, request domain.AdminControlRequest) {
	t.Helper()
	changed := request
	changed.Reason = "different business content"
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("different content reused an actor-scoped key", err)
	}
	changed = request
	changed.Key = "management-stale-0001"
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("stale revision changed the desired target", err)
	}
	for _, proof := range []struct {
		token string
		csrf  string
	}{
		{
			token: viewer.Token,
			csrf:  viewer.CSRF,
		},
		{
			token: actor.Token,
			csrf:  viewer.CSRF,
		},
		{token: actor.Token},
	} {
		if _, err := controls.Control(ctx, proof.token, proof.csrf, request); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("role or independent CSRF failed open", err)
		}
	}
}

func TestAdminControlAuditFailureRollsBackAndConcurrentReplayHasOneEffect(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actor, controls := adminControlFixture(ctx, t, db)
	request := domain.AdminControlRequest{
		Key:              "management-pause-0001",
		StrategyID:       submit.Input.Authority.StrategyID,
		ExpectedRevision: 1,
		Action:           domain.AdminControlPause,
		Reason:           "pause for operator review",
	}
	const callback = "test:reject-control-audit"
	failure := errors.New("controlled audit failure")
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, audit := tx.Statement.Dest.(*modelv1.AdminAudit); audit {
			tx.Error = failure
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, failure) || !errors.Is(err, domain.ErrAdminControlUnavailable) {
		t.Fatal("audit failure was hidden or treated as accepted", err)
	}
	assertControlInstance(ctx, t, db, request.StrategyID, domain.StrategyStateRunning, domain.StrategyStateRunning, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 0)
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", request.StrategyID).Take(&risk).Error; err != nil || !risk.AllowNewRisk || risk.Revision != 1 {
		t.Fatal("failed audit committed a risk change", err)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	assertConcurrentAdminControl(ctx, t, controls, actor, request)
	assertControlInstance(ctx, t, db, request.StrategyID, domain.StrategyStateRunning, domain.StrategyStatePaused, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 1)
	var audits int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("action = ?", domain.AdminControlPause).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("concurrent replay created duplicate management audit", err)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
}

func assertConcurrentAdminControl(ctx context.Context, t *testing.T, controls domain.AdminStrategyControl, actor domain.AdminLogin, request domain.AdminControlRequest) {
	t.Helper()
	start := make(chan struct{})
	type response struct {
		operation domain.AdminOperation
		err       error
	}
	results := make(chan response, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			operation, err := controls.Control(ctx, actor.Token, actor.CSRF, request)
			results <- response{
				operation: operation,
				err:       err,
			}
		})
	}
	close(start)
	workers.Wait()
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.operation.ID <= 0 || first.operation.ID != second.operation.ID {
		t.Fatal("concurrent actor/key did not converge to one operation", first.err, second.err)
	}
}

func adminControlFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, domain.AdminStrategyControl) {
	t.Helper()
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminProvisionRequest(ctx, t, auth, "control-operator", domain.AdminRoleOperator); err != nil {
		t.Fatal(err)
	}
	actor, err := adminLoginRequest(ctx, t, auth, "control-operator", adminFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		t.Fatal(err)
	}
	return actor, controls
}

func assertControlInstance(ctx context.Context, t *testing.T, db *gorm.DB, id int64, actual, desired domain.StrategyState, revision int64) {
	t.Helper()
	var instance modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).Take(&instance).Error; err != nil || instance.State != string(actual) || instance.DesiredState != string(desired) || instance.Revision != revision {
		t.Fatalf("control changed actual state or lost target/revision: state=%s desired=%s revision=%d err=%v", instance.State, instance.DesiredState, instance.Revision, err)
	}
}

func assertAdminOperationCompleted(ctx context.Context, t *testing.T, db *gorm.DB, controls domain.AdminStrategyControl, observer domain.AdminOperationObserver, token string, id int64) {
	t.Helper()
	if changed, err := observer.Observe(ctx, id); err != nil || !changed {
		t.Fatal("proved terminal state was not recorded", err)
	}
	result, err := controls.Operation(ctx, token, id)
	if err != nil || result.State != domain.AdminOperationSucceeded || result.CompletedAt.IsZero() || result.Revision != 2 {
		t.Fatal("terminal operation result was not durable", err)
	}
	if changed, err := observer.Observe(ctx, id); err != nil || changed {
		t.Fatal("terminal result was observed twice", err)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).UpdateColumn("reason", "rewritten").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).Delete(&modelv1.AdminStrategyOperation{}).Error, "23514")
}

func assertAdminOperationCannotComplete(ctx context.Context, t *testing.T, db *gorm.DB, operationID, strategyID int64) {
	t.Helper()
	var strategy modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).Take(&strategy).Error; err != nil {
		t.Fatal(err)
	}
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", operationID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", operationID).
		Select("State", "OutcomeCode", "Revision", "CompletedAt", "ObservedRevision", "ObservedState", "ObservedOwnerId", "ObservedOwnerEpoch").
		Updates(&modelv1.AdminStrategyOperation{
			State:              string(domain.AdminOperationSucceeded),
			OutcomeCode:        domain.AdminOperationOutcomeReached,
			Revision:           2,
			CompletedAt:        proto.Int64(clock.NowMS),
			ObservedRevision:   proto.Int64(strategy.Revision),
			ObservedState:      proto.String(strategy.State),
			ObservedOwnerId:    proto.String(strategy.RuntimeOwnerId),
			ObservedOwnerEpoch: proto.Int64(strategy.RuntimeOwnerEpoch),
		}).Error
	assertDatabaseCode(t, err, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", operationID).
		UpdateColumn("idempotency_key", "rewritten-operation-key").Error, "23514")
}
