//go:build integration

package migrate

import (
	"context"
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

func TestAdminFlatteningSchemaKeepsExactScopedConfirmationAndImmutableHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, row := flattenSchemaFixture(ctx, t, db)
	before := publicationInstance(ctx, t, db, row.StrategyInstanceId)
	for _, mutate := range []func(*modelv1.AdminAccountFlattening){
		func(r *modelv1.AdminAccountFlattening) { r.Scope = "strategy" },
		func(r *modelv1.AdminAccountFlattening) { r.ExpectedRiskRevision++ },
		func(r *modelv1.AdminAccountFlattening) { r.ExpectedPortfolioRevision++ },
		func(r *modelv1.AdminAccountFlattening) { r.ExpectedBaseRevision++ },
		func(r *modelv1.AdminAccountFlattening) { r.ConfirmedQuantity = "9" },
		func(r *modelv1.AdminAccountFlattening) { r.ConfirmedQuantity = "NaN" },
		func(r *modelv1.AdminAccountFlattening) { r.LimitPrice = "0" },
		func(r *modelv1.AdminAccountFlattening) { r.LimitPrice = "10.011" },
		func(r *modelv1.AdminAccountFlattening) { r.RequestFingerprint = "invalid" },
		func(r *modelv1.AdminAccountFlattening) { r.IdempotencyKey = "short" },
		func(r *modelv1.AdminAccountFlattening) { r.Reason = "different from audit" },
		func(r *modelv1.AdminAccountFlattening) { r.DeleteAt = proto.Int64(time.Now().UnixMilli()) },
	} {
		candidate := proto.CloneOf(row)
		mutate(candidate)
		assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(candidate).Error, "23514")
	}
	missingAccount := proto.CloneOf(row)
	missingAccount.AccountId = "another-account"
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(missingAccount).Error, "23503")
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(row).Error; err != nil {
		t.Fatal("valid generated flatten confirmation rejected", err)
	}
	var stored modelv1.AdminAccountFlattening
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", row.Id).Take(&stored).Error; err != nil ||
		stored.Id <= 0 || stored.CreateAt <= 0 || stored.UpdateAt <= 0 || stored.DeleteAt != nil || stored.AccountId != submit.Input.AccountID {
		t.Fatal("generated flatten identity/common fields did not persist", err)
	}
	quantity, qErr := domain.ParseLedgerDecimal(stored.ConfirmedQuantity, false, true)
	price, pErr := decimal.NewFromString(stored.LimitPrice)
	if qErr != nil || pErr != nil || !quantity.Equal(decimal.NewFromInt(10)) || !price.Equal(decimal.RequireFromString("10.01")) {
		t.Fatal("flatten decimal confirmation did not round trip", qErr, pErr)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", row.Id).UpdateColumn("reason", "rewrite").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", row.Id).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", row.Id).Delete(&modelv1.AdminAccountFlattening{}).Error, "23514")
	assertFlattenSchemaKeysAndIdentity(ctx, t, db, row)
	if !proto.Equal(before, publicationInstance(ctx, t, db, row.StrategyInstanceId)) {
		t.Fatal("structural flatten confirmation changed strategy state/configuration")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func assertFlattenSchemaKeysAndIdentity(ctx context.Context, t *testing.T, db *gorm.DB, row *modelv1.AdminAccountFlattening) {
	t.Helper()
	var original modelv1.AdminAudit
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", row.AuditId).Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	audit := proto.CloneOf(&original)
	audit.Id = 0
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		t.Fatal(err)
	}
	next := proto.CloneOf(row)
	next.Id, next.AuditId, next.IdempotencyKey = 0, audit.Id, "flatten-schema-request-0002"
	keyConflict := proto.CloneOf(next)
	keyConflict.IdempotencyKey = row.IdempotencyKey
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(keyConflict).Error, "23505")
	auditConflict := proto.CloneOf(next)
	auditConflict.AuditId = row.AuditId
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(auditConflict).Error, "23505")
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(next).Error; err != nil || next.Id <= row.Id || next.DeleteAt != nil {
		t.Fatal("new scoped confirmation did not obtain a distinct generated identity", err)
	}
}

func TestAdminFlatteningSchemaRechecksAdministratorAndBalanceAtCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, _, row := flattenSchemaFixture(ctx, t, db)
	err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(proto.CloneOf(row)).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", admin.Principal.SessionID).UpdateColumn("revoked_at", time.Now().UnixMilli()).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_flatten_authority")
	var session modelv1.AdminSession
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", admin.Principal.SessionID).Take(&session).Error; err != nil || session.RevokedAt != nil {
		t.Fatal("rejected flatten confirmation did not roll back revocation", err)
	}
	err = db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(proto.CloneOf(row)).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", row.AccountId, "BTC").
			Select("Available", "Revision").Updates(&modelv1.SimulatorBalance{
			Available: "9",
			Revision:  row.ExpectedBaseRevision + 1,
		}).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_flatten_confirmation")
	for index, role := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator} {
		actor := riskActorFixture(ctx, t, db, "flatten-schema-"+string(role), role, byte(243+index), domain.AdminSessionTTL)
		candidate := proto.CloneOf(row)
		candidate.ActorUserId, candidate.ActorSessionId = actor.Principal.UserID, actor.Principal.SessionID
		assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(candidate).Error, "admin_flatten_authority")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAccountFlattening{}, 0)
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(row).Error; err != nil {
		t.Fatal("rolled-back confirmation did not restore exact original balance/authority", err)
	}
}

func TestAdminFlatteningSchemaRejectsCurrentSharedAccountDespiteEarlierConfirmation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, submit, row := flattenSchemaFixture(ctx, t, db)
	creator, err := repository.NewAdminStrategyCreator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(ctx, admin.Token, admin.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); err != nil {
		t.Fatal(err)
	}
	assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(row).Error, "admin_flatten_scope")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAccountFlattening{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningUpgradeAddsNoRequestsAndKeepsSchema34RiskFacts(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:34]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	source := riskEventLegacySource(ctx, t, db, instrument.Id, false)
	before := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if version, err := Apply(ctx, db); err != nil || version != 37 {
		t.Fatal("schema34 flatten upgrade failed", err)
	}
	after := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if !proto.Equal(source, riskEventSourceRow(ctx, t, db, source.StrategyInstanceId)) || len(before) != 1 || len(after) != 1 || !proto.Equal(before[0], after[0]) {
		t.Fatal("flatten schema rewrote existing risk facts")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAccountFlattening{}, 0)
	if version, err := Apply(ctx, db); err != nil || version != 37 {
		t.Fatal("checked flatten upgrade replay failed", err)
	}
}

func flattenSchemaFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, domain.GatewaySubmit, *modelv1.AdminAccountFlattening) {
	t.Helper()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	return flattenSchemaCandidate(ctx, t, db, operator, submit)
}

func flattenSchemaCandidate(ctx context.Context, t *testing.T, db *gorm.DB, operator domain.AdminLogin, submit domain.GatewaySubmit) (domain.AdminLogin, domain.GatewaySubmit, *modelv1.AdminAccountFlattening) {
	t.Helper()
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, operator.Token, operator.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	if work, err := emergencyProgressStore(t, db).Prepare(ctx, accepted.StrategyID, 0); err != nil || work.State != domain.AdminEmergencyCancelSucceeded {
		t.Fatal("flatten schema fixture lacks real completed cancellation proof", err)
	}
	admin := riskActorFixture(ctx, t, db, "flatten-schema-admin", domain.AdminRoleAdmin, 241, domain.AdminSessionTTL)
	var portfolio modelv1.PortfolioAccount
	if err := db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", submit.Input.AccountID).Take(&portfolio).Error; err != nil {
		t.Fatal(err)
	}
	var balance modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", submit.Input.AccountID, portfolio.BaseAsset).Take(&balance).Error; err != nil {
		t.Fatal(err)
	}
	reason := "explicit isolated account flatten confirmation"
	audit := &modelv1.AdminAudit{
		Actor:         admin.Principal.Username,
		ActorRole:     string(admin.Principal.Role),
		Action:        domain.AdminFlattenAction,
		Resource:      domain.AdminFlattenAuditResource + ":" + submit.Input.AccountID + ":" + strconv.FormatInt(submit.Input.InstrumentID, 10),
		Reason:        reason,
		BeforeVersion: proto.Int64(portfolio.Revision),
		AfterVersion:  proto.Int64(portfolio.Revision),
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		t.Fatal(err)
	}
	return admin, submit, &modelv1.AdminAccountFlattening{
		ActorUserId:               admin.Principal.UserID,
		ActorSessionId:            admin.Principal.SessionID,
		StrategyInstanceId:        accepted.StrategyID,
		AccountId:                 submit.Input.AccountID,
		InstrumentId:              submit.Input.InstrumentID,
		EmergencyCancellationId:   accepted.ID,
		AuditId:                   audit.Id,
		IdempotencyKey:            "flatten-schema-request-0001",
		RequestFingerprint:        strings.Repeat("a", 64),
		Scope:                     string(domain.AdminFlattenScopeAccountInstrument),
		ExpectedRiskRevision:      accepted.RiskRevision,
		ExpectedPortfolioRevision: portfolio.Revision,
		ExpectedBaseRevision:      balance.Revision,
		ConfirmedQuantity:         portfolio.Quantity,
		LimitPrice:                "10.01",
		Reason:                    reason,
	}
}
