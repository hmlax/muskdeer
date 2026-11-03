//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminEmergencyProgressEmptyScopeNeedsExplicitEnrollmentBeforeRecovery(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	store := emergencyAcceptanceStore(t, db)
	accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	var task modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.ID).Take(&task).Error; err != nil ||
		task.State != string(domain.AdminEmergencyCancelAccepted) || task.EnrollmentUpperId != nil || task.EnrollmentComplete || task.CompletedAt != nil {
		t.Fatal("empty scope was silently completed or lacked its durable marker", err)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ?", task.Id).
		Updates(&modelv1.AdminEmergencyCancellationTask{
			State:       string(domain.AdminEmergencyCancelSucceeded),
			Revision:    2,
			CompletedAt: proto.Int64(time.Now().UnixMilli()),
		}).Error, "23514")
	work := emergencyProgressStore(t, db)
	result, err := work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || result.AcceptanceID != accepted.ID || result.State != domain.AdminEmergencyCancelSucceeded || !result.Progress.EnrollmentComplete || result.Progress.Targets != 0 || result.CompletedAt.IsZero() {
		t.Fatal("explicit proven empty enrollment did not complete", err)
	}
	admin := riskActorFixture(ctx, t, db, "emergency-progress-admin", domain.AdminRoleAdmin, 171, domain.AdminSessionTTL)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	instance := publicationInstance(ctx, t, db, accepted.StrategyID)
	if restored, err := riskControlFixture(t, db).Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(instance)); err != nil || !restored.Enabled {
		t.Fatal("durable empty completion did not permit otherwise valid admin recovery", err)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ?", result.TaskID).
		Select("State", "Revision").Updates(&modelv1.AdminEmergencyCancellationTask{
		State:    string(domain.AdminEmergencyCancelAccepted),
		Revision: result.Revision + 1,
	}).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ?", result.TaskID).Delete(&modelv1.AdminEmergencyCancellationTask{}).Error, "23514")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminEmergencyTaskCannotSkipReservedTargetOrDelegateOutsideRecordedPrefix(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	var task modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.ID).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ?", task.Id).
		Select("EnrollmentUpperId", "EnrollmentAfterId", "EnrollmentComplete", "Revision").Updates(&modelv1.AdminEmergencyCancellationTask{
		EnrollmentUpperId:  proto.Int64(order.ID),
		EnrollmentAfterId:  order.ID,
		EnrollmentComplete: true,
		Revision:           2,
	}).Error
	assertEmergencySchemaConstraint(t, err, "admin_emergency_task_enrollment")
	err = db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Transaction(func(tx *gorm.DB) error {
		audit := &modelv1.AdminAudit{
			Actor:         actor.Principal.Username,
			ActorRole:     string(actor.Principal.Role),
			Action:        domain.AdminCancellationAction,
			Resource:      domain.AdminAuditResourceOrder + ":" + strconv.FormatInt(order.ID, 10),
			Reason:        accepted.Reason,
			BeforeVersion: proto.Int64(order.Revision),
			AfterVersion:  proto.Int64(order.Revision),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Create(&modelv1.AdminOrderCancellation{
			ActorUserId:             actor.Principal.UserID,
			ActorSessionId:          actor.Principal.SessionID,
			StrategyInstanceId:      accepted.StrategyID,
			OrderIntentId:           order.ID,
			AuditId:                 audit.Id,
			IdempotencyKey:          "emergency-prefix-bypass-0001",
			RequestFingerprint:      strings.Repeat("a", 64),
			ExpectedOrderRevision:   order.Revision,
			Reason:                  accepted.Reason,
			State:                   string(domain.AdminCancellationAccepted),
			OutcomeCode:             domain.AdminCancellationPending,
			Revision:                1,
			EmergencyCancellationId: proto.Int64(accepted.ID),
		}).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_cancellation_emergency_scope")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
}

func TestAdminEmergencyEnrollmentIsBoundedAndContinuesOnlyAcceptedScopeAfterLogout(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	for index := range domain.MaxEmergencyTargetsPerStep + 1 {
		refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
		input := submit.Input
		input.IdempotencyKey = "emergency-enrollment-intent-" + strconv.Itoa(index)
		input.Quantity = decimal.RequireFromString("0.1")
		input.Deadline = time.Now().Add(time.Second)
		if _, err := intents.Accept(ctx, input, submit.ExecutionAuthority); err != nil {
			t.Fatal("could not seed a real reserved unsent target", err)
		}
	}
	budget := activationBudget(ctx, t, db, submit.Input.Authority.StrategyID)
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	work := emergencyProgressStore(t, db)
	first, err := work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || first.State != domain.AdminEmergencyCancelAccepted || first.Progress.Targets != int64(domain.MaxEmergencyTargetsPerStep) || first.Progress.EnrollmentComplete {
		t.Fatal("one bounded enrollment consumed too much or reauthorized a revoked session", err)
	}
	last, err := work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || last.Progress.Targets != int64(domain.MaxEmergencyTargetsPerStep+1) || !last.Progress.EnrollmentComplete || last.State != domain.AdminEmergencyCancelAccepted || last.Progress.Terminal != 0 {
		t.Fatal("next enrollment lost cursor or treated queued cancels as terminal", err)
	}
	again, err := work.Prepare(ctx, accepted.StrategyID, 0)
	if err != nil || again.Progress.Targets != last.Progress.Targets || again.State != domain.AdminEmergencyCancelAccepted {
		t.Fatal("repeat enrollment duplicated targets or completed unknown obligations", err)
	}
	var children []*modelv1.AdminOrderCancellation
	if err := db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("emergency_cancellation_id = ?", accepted.ID).Order("id ASC").Find(&children).Error; err != nil || len(children) != domain.MaxEmergencyTargetsPerStep+1 {
		t.Fatal("accepted scope did not retain typed child requests", err)
	}
	for _, child := range children {
		if child.ActorUserId != actor.Principal.UserID || child.ActorSessionId != actor.Principal.SessionID || child.StrategyInstanceId != accepted.StrategyID || child.State != string(domain.AdminCancellationAccepted) || child.Reason != accepted.Reason {
			t.Fatal("child expanded the durable actor/scope/reason authorization")
		}
	}
	if current := activationBudget(ctx, t, db, accepted.StrategyID); current.ReservedNotional != budget.ReservedNotional {
		t.Fatal("enrollment released unsent reservations without execution proof")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	if _, err := emergencyAcceptanceStore(t, db).Acceptance(ctx, actor.Token, accepted.StrategyID, accepted.ID); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("durable processing accidentally re-enabled revoked reads", err)
	}
}

func emergencyProgressStore(t *testing.T, db *gorm.DB) domain.StrategyEmergencyCancellations {
	t.Helper()
	work, err := repository.NewStrategyEmergencyCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	return work
}
