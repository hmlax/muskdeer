//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminEmergencyAcceptanceSchemaBindsRiskClosureScopeAuditAndImmutableHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	sibling, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	row := emergencySchemaCandidate(ctx, t, db, actor, before.Id, 1)
	assertEmergencySchemaInvalidRows(ctx, t, db, row)
	wrongScope := proto.CloneOf(row)
	wrongScope.StrategyInstanceId = sibling.StrategyID
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(wrongScope).Error, "23514")
	var risk modelv1.AdminStrategyRiskChange
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Where("id = ?", row.RiskChangeId).Take(&risk).Error; err != nil {
		t.Fatal(err)
	}
	wrongAudit := proto.CloneOf(row)
	wrongAudit.AuditId = risk.AuditId
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(wrongAudit).Error, "23514")
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(row).Error; err != nil {
		t.Fatal("valid generated emergency acceptance rejected", err)
	}
	second := emergencySchemaCandidate(ctx, t, db, actor, before.Id, 2)
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(second).Error; err != nil {
		t.Fatal("second generated acceptance rejected", err)
	}
	if row.Id <= 0 || second.Id <= 0 || row.Id == second.Id || row.CreateAt <= 0 || row.UpdateAt <= 0 || row.DeleteAt != nil {
		t.Fatal("generated common identity/time fields were not persisted")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Where("id = ?", row.Id).UpdateColumn("reason", "rewritten history").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Where("id = ?", row.Id).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Where("id = ?", row.Id).Delete(&modelv1.AdminEmergencyCancellation{}).Error, "23514")
	assertEmergencySchemaUniqueKeys(ctx, t, db, actor, before.Id, row)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("acceptance schema changed strategy state/configuration")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminEmergencyAcceptanceUpgradeDoesNotInventRequestsOrRewriteExistingRiskHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:31]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	source := riskEventLegacySource(ctx, t, db, instrument.Id, false)
	events := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(events) != 1 {
		t.Fatal("schema31 fixture lacks its real risk event")
	}
	if version, err := Apply(ctx, db); err != nil || version != 33 {
		t.Fatal("emergency schema upgrade failed", err)
	}
	if !proto.Equal(source, riskEventSourceRow(ctx, t, db, source.StrategyInstanceId)) {
		t.Fatal("acceptance upgrade rewrote old risk source")
	}
	after := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(after) != len(events) || !proto.Equal(after[0], events[0]) {
		t.Fatal("acceptance upgrade invented risk history")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 0)
	if version, err := Apply(ctx, db); err != nil || version != 33 {
		t.Fatal("checked acceptance upgrade replay failed", err)
	}
}

func TestAdminEmergencyAcceptanceCommitRechecksCurrentAuthorityAndRollsBackRevocation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	row := emergencySchemaCandidate(ctx, t, db, actor, submit.Input.Authority.StrategyID, 1)
	err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(proto.CloneOf(row)).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", actor.Principal.SessionID).UpdateColumn("revoked_at", time.Now().UnixMilli()).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_emergency_authority")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 0)
	var session modelv1.AdminSession
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", actor.Principal.SessionID).Take(&session).Error; err != nil || session.RevokedAt != nil {
		t.Fatal("deferred acceptance failure did not roll back transaction", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(row).Error; err != nil {
		t.Fatal("rolled-back authority could not accept a valid record", err)
	}
}

func TestAdminEmergencyAcceptanceRejectsReopenedRiskAndViewerAtCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	row := emergencySchemaCandidate(ctx, t, db, actor, submit.Input.Authority.StrategyID, 1)
	// Explicit database fault injection: a prior block witness must not authorize
	// acceptance after its actual flag is open. Transaction failure restores it.
	err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(proto.CloneOf(row)).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", row.StrategyInstanceId).UpdateColumn("enabled", true).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_emergency_scope")
	viewer := riskActorFixture(ctx, t, db, "emergency-schema-viewer", domain.AdminRoleViewer, 141, domain.AdminSessionTTL)
	wrongActor := proto.CloneOf(row)
	wrongActor.ActorUserId, wrongActor.ActorSessionId = viewer.Principal.UserID, viewer.Principal.SessionID
	assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(wrongActor).Error, "admin_emergency_authority")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 0)
	state, err := riskControlFixture(t, db).State(ctx, actor.Token, row.StrategyInstanceId)
	if err != nil || state.Enabled || state.Revision != row.RiskRevision {
		t.Fatal("rejected acceptance did not restore its prior risk state", err)
	}
}

func assertEmergencySchemaConstraint(t *testing.T, err error, name string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.ConstraintName != name {
		t.Fatalf("expected deferred constraint %s: %v", name, err)
	}
}

func emergencySchemaCandidate(ctx context.Context, t *testing.T, db *gorm.DB, actor domain.AdminLogin, strategyID, revision int64) *modelv1.AdminEmergencyCancellation {
	t.Helper()
	key := "emergency-schema-fixture-" + strconv.FormatInt(revision, 10)
	reason := "explicit strategy emergency schema fixture"
	change, err := riskControlFixture(t, db).Change(ctx, actor.Token, actor.CSRF, domain.AdminRiskRequest{
		Key:              key,
		StrategyID:       strategyID,
		ExpectedRevision: revision,
		Action:           domain.AdminRiskBlock,
		Reason:           reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	audit := &modelv1.AdminAudit{
		Actor:         actor.Principal.Username,
		ActorRole:     string(actor.Principal.Role),
		Action:        domain.AdminEmergencyCancelAction,
		Resource:      domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(strategyID, 10),
		Reason:        reason,
		BeforeVersion: proto.Int64(revision),
		AfterVersion:  proto.Int64(change.Revision),
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		t.Fatal(err)
	}
	return &modelv1.AdminEmergencyCancellation{
		ActorUserId:          actor.Principal.UserID,
		ActorSessionId:       actor.Principal.SessionID,
		StrategyInstanceId:   strategyID,
		RiskChangeId:         change.ID,
		AuditId:              audit.Id,
		IdempotencyKey:       key,
		RequestFingerprint:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
		ExpectedRiskRevision: revision,
		RiskRevision:         change.Revision,
		Reason:               reason,
	}
}

func assertEmergencySchemaInvalidRows(ctx context.Context, t *testing.T, db *gorm.DB, row *modelv1.AdminEmergencyCancellation) {
	t.Helper()
	for _, change := range []func(*modelv1.AdminEmergencyCancellation){
		func(v *modelv1.AdminEmergencyCancellation) { v.Scope = "account" },
		func(v *modelv1.AdminEmergencyCancellation) { v.Scope = "" },
		func(v *modelv1.AdminEmergencyCancellation) { v.ExpectedRiskRevision = 0 },
		func(v *modelv1.AdminEmergencyCancellation) { v.RiskRevision++ },
		func(v *modelv1.AdminEmergencyCancellation) { v.Reason = "not the audit reason" },
		func(v *modelv1.AdminEmergencyCancellation) { v.RequestFingerprint = "invalid" },
		func(v *modelv1.AdminEmergencyCancellation) { v.IdempotencyKey = "short" },
		func(v *modelv1.AdminEmergencyCancellation) { v.DeleteAt = proto.Int64(time.Now().UnixMilli()) },
	} {
		invalid := proto.CloneOf(row)
		change(invalid)
		assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(invalid).Error, "23514")
	}
}

func assertEmergencySchemaUniqueKeys(ctx context.Context, t *testing.T, db *gorm.DB, actor domain.AdminLogin, strategyID int64, first *modelv1.AdminEmergencyCancellation) {
	t.Helper()
	third := emergencySchemaCandidate(ctx, t, db, actor, strategyID, 3)
	for _, change := range []func(*modelv1.AdminEmergencyCancellation){
		func(v *modelv1.AdminEmergencyCancellation) { v.IdempotencyKey = first.IdempotencyKey },
		func(v *modelv1.AdminEmergencyCancellation) { v.RiskChangeId = first.RiskChangeId },
		func(v *modelv1.AdminEmergencyCancellation) { v.AuditId = first.AuditId },
	} {
		duplicate := proto.CloneOf(third)
		change(duplicate)
		assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(duplicate).Error, "23505")
	}
}
