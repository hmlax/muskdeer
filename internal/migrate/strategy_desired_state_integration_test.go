//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyDesiredTargetPersistsIndependentlyOfRuntimeState(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	id := submit.Input.Authority.StrategyID
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStateRunning, true)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStateStopped, false)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_state", domain.StrategyStatePaused).Error; err != nil {
		t.Fatal(err)
	}
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStatePaused, false)
	// Legacy explicit bool-only resume/stop requests retain their safe meaning.
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_running", true).Error; err != nil {
		t.Fatal(err)
	}
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStateRunning, true)
	for _, target := range []domain.StrategyState{domain.StrategyStateRecovering, domain.StrategyStatePaused, ""} {
		assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_state", target).Error, "23514")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_state", nil).Error, "23502")
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStateRunning, true)
}

func assertStrategyDesiredState(ctx context.Context, t *testing.T, db *gorm.DB, id int64, target domain.StrategyState, running bool) {
	t.Helper()
	var row struct {
		DesiredState   domain.StrategyState
		DesiredRunning bool
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Select("desired_state", "desired_running").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.DesiredState != target || row.DesiredRunning != running {
		t.Fatalf("requested state changed independently of its explicit target: state=%s running=%t", row.DesiredState, row.DesiredRunning)
	}
}

func TestStrategyPauseTargetSurvivesRecoveryTakeoverAndDistinctStop(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	controller, execution, market, submit := gridControllerFixture(ctx, t, db)
	orders := waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	cancels := execution.cancels
	id := submit.Input.Authority.StrategyID
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).
		Select("DesiredRunning", "DesiredState").Updates(&modelv1.StrategyInstance{
		DesiredRunning: false,
		DesiredState:   string(domain.StrategyStatePaused),
	}).Error; err != nil {
		t.Fatal(err)
	}
	runGridController(ctx, t, controller, market, "100", 12)
	expireLeaseFixture(ctx, t, db, lifecycleToken(submit.Input.Authority).Key)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	replacement := strategyControllerWith(ctx, t, db, submit, leases, execution, market)
	strategyControllerTick(ctx, t, replacement, market)
	var instance modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, id).Error; err != nil || instance.State != string(domain.StrategyStateRecovering) {
		t.Fatal("replacement did not claim a recovering runtime")
	}
	assertStrategyDesiredState(ctx, t, db, id, domain.StrategyStatePaused, false)
	runGridController(ctx, t, replacement, market, "100", 6)
	for _, order := range orders {
		if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	runGridController(ctx, t, replacement, market, "100", 8)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, id).Error; err != nil || instance.State != string(domain.StrategyStatePaused) || execution.cancels-cancels != len(orders) {
		t.Fatal("takeover lost the pause target or replayed prior cancel requests")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).UpdateColumn("desired_state", domain.StrategyStateStopped).Error; err != nil {
		t.Fatal(err)
	}
	runGridController(ctx, t, replacement, market, "100", 2)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, id).Error; err != nil || instance.State != string(domain.StrategyStateStopped) {
		t.Fatal("explicit stop was confused with the former pause target")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).
		Select("DesiredRunning", "DesiredState").Updates(&modelv1.StrategyInstance{
		DesiredRunning: true,
		DesiredState:   string(domain.StrategyStateRunning),
	}).Error; err != nil {
		t.Fatal(err)
	}
	orders = waitGridControllerQuotes(ctx, t, replacement, execution, market, submit)
	stopGridController(ctx, t, db, replacement, execution, market, submit, orders)
}

func TestStrategyDesiredTargetRejectsWrongTerminalStateAtDatabaseCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.Instance(ctx, submit.Input.Authority.StrategyID)
	if err != nil {
		t.Fatal(err)
	}
	token := lifecycleToken(submit.Input.Authority)
	if _, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateRecovering}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", current.ID).
		Select("DesiredRunning", "DesiredState").Updates(&modelv1.StrategyInstance{
		DesiredRunning: false,
		DesiredState:   string(domain.StrategyStatePaused),
	}).Error; err != nil {
		t.Fatal(err)
	}
	current, err = lifecycle.Instance(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", current.ID).UpdateColumn("state", domain.StrategyStateStopped).Error
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.ConstraintName != "strategy_runtime_transition" {
		t.Fatal("final SQL gate accepted a stop for a requested pause")
	}
	retained, err := lifecycle.Instance(ctx, current.ID)
	if err != nil || retained != current {
		t.Fatal("rejected terminal transition changed persisted runtime or target")
	}
	paused, err := lifecycle.Transition(ctx, retained, token, domain.StrategyTransition{State: domain.StrategyStatePaused})
	if err != nil || paused.State != domain.StrategyStatePaused || paused.DesiredState != domain.StrategyStatePaused {
		t.Fatal("matching requested terminal state was not available after rollback")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
}
