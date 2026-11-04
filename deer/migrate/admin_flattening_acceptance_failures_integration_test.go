//go:build integration

package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminFlatteningAcceptanceRequiresCurrentAdminCSRFAndExactConfirmation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, _, request := flattenAcceptanceFixture(ctx, t, db)
	store := flattenAcceptanceStore(t, db)
	for index, role := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator} {
		actor := riskActorFixture(ctx, t, db, "flatten-acceptance-"+string(role), role, byte(244+index), domain.AdminSessionTTL)
		if _, err := store.Request(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("non-administrator submitted an inventory sale confirmation", err)
		}
	}
	if _, err := store.Request(ctx, admin.Token, "", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("flatten request omitted independent CSRF", err)
	}
	for _, mutate := range []func(*domain.AdminFlattenRequest){
		func(r *domain.AdminFlattenRequest) { r.ExpectedRiskRevision++ },
		func(r *domain.AdminFlattenRequest) { r.ExpectedPortfolioRevision++ },
		func(r *domain.AdminFlattenRequest) { r.ExpectedBaseRevision++ },
		func(r *domain.AdminFlattenRequest) {
			r.ConfirmedQuantity = r.ConfirmedQuantity.Sub(r.ConfirmedQuantity)
		},
	} {
		changed := request
		mutate(&changed)
		if _, err := store.Request(ctx, admin.Token, admin.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
			t.Fatal("stale inventory or independent revision was accepted", err)
		}
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
	assertFlatAuditCount(ctx, t, db, 0)
	receipt, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, admin.Token, admin.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Request(ctx, admin.Token, admin.CSRF, request); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old original key bypassed actual logout", err)
	}
	if _, err := store.Confirmation(ctx, admin.Token, request.StrategyID, receipt.ID); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked session read an old confirmation", err)
	}
	fresh, err := auth.Login(ctx, admin.Principal.Username, adminFixturePassword)
	if err != nil || fresh.Principal.UserID != admin.Principal.UserID || fresh.Principal.SessionID == admin.Principal.SessionID {
		t.Fatal("actual re-login did not create current authority for the same actor", err)
	}
	if replay, err := store.Request(ctx, fresh.Token, fresh.CSRF, request); err != nil || replay.ID != receipt.ID || !replay.ConfirmedQuantity.Equal(receipt.ConfirmedQuantity) {
		t.Fatal("current same-actor session could not replay immutable confirmation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 1)
	assertFlatAuditCount(ctx, t, db, 1)
}

func TestAdminFlatteningAcceptanceAuditAndReceiptFailuresRollBack(t *testing.T) {
	for _, stage := range []string{"audit", "receipt"} {
		t.Run(stage, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			admin, submit, request := flattenAcceptanceFixture(ctx, t, db)
			store := flattenAcceptanceStore(t, db)
			name := "flatten-acceptance-failure-" + stage
			failure := errors.New("private flatten storage fault")
			if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				audit, isAudit := tx.Statement.Dest.(*commonmodelv1.AdminAudit)
				_, isReceipt := tx.Statement.Dest.(*executionmodelv1.AdminAccountFlattening)
				if stage == "audit" && isAudit && audit.Action == domain.AdminFlattenAction || stage == "receipt" && isReceipt {
					tx.Error = tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			result, err := store.Request(ctx, admin.Token, admin.CSRF, request)
			if err == nil || result.ID != 0 || strings.Contains(err.Error(), "private") {
				t.Fatal("storage failure returned success, partial receipt or private data", err)
			}
			if err := db.Callback().Create().Remove(name); err != nil {
				t.Fatal(err)
			}
			assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
			assertFlatAuditCount(ctx, t, db, 0)
			assertGatewayOrderCount(ctx, t, db, 0)
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
			if _, err := store.Request(ctx, admin.Token, admin.CSRF, request); err != nil {
				t.Fatal("rolled-back request could not later persist the original key", err)
			}
			assertFlatAuditCount(ctx, t, db, 1)
		})
	}
}

func TestAdminFlatteningAcceptanceRejectsSharedUnreconciledAndUncompletedScope(t *testing.T) {
	for _, scenario := range []string{"shared", "projection", "unfinished-scope"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			admin, submit, request := flattenAcceptanceFixture(ctx, t, db)
			switch scenario {
			case "shared":
				creator, err := repository.NewAdminStrategyCreator(db)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := creator.Create(ctx, admin.Token, admin.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); err != nil {
					t.Fatal(err)
				}
			case "projection":
				if err := db.WithContext(ctx).Model(&portfoliomodelv1.PortfolioAccount{}).Where("account_id = ?", request.AccountID).UpdateColumn("reconciled", false).Error; err != nil {
					t.Fatal(err)
				}
			case "unfinished-scope":
				emergency := emergencyAcceptanceRequest(request.StrategyID)
				emergency.Key, emergency.ExpectedRiskRevision = "flatten-uncompleted-scope-0001", request.ExpectedRiskRevision
				accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, admin.Token, admin.CSRF, emergency)
				if err != nil {
					t.Fatal(err)
				}
				request.CancellationID, request.ExpectedRiskRevision = accepted.ID, accepted.RiskRevision
			}
			if result, err := flattenAcceptanceStore(t, db).Request(ctx, admin.Token, admin.CSRF, request); !errors.Is(err, domain.ErrAdminFlattenDenied) || result.ID != 0 {
				t.Fatal("unsafe current account qualification returned a flatten confirmation", err)
			}
			assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
			assertFlatAuditCount(ctx, t, db, 0)
			assertGatewayOrderCount(ctx, t, db, 0)
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
		})
	}
}

func TestAdminFlatteningAcceptanceCommitRevocationReturnsNoReceiptAndRollsBack(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, _, request := flattenAcceptanceFixture(ctx, t, db)
	store := flattenAcceptanceStore(t, db)
	const callback = "flatten-revoke-after-receipt"
	var revokedRows int64
	var revokedFailure error
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, receipt := tx.Statement.Dest.(*executionmodelv1.AdminAccountFlattening); receipt {
			// Apply context first, then reset the create statement while keeping
			// its transaction; inherited INSERT SQL must never be executed again.
			update := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&commonmodelv1.AdminSession{}).
				Where("id = ?", admin.Principal.SessionID).UpdateColumn("revoked_at", time.Now().UnixMilli())
			revokedRows, revokedFailure = update.RowsAffected, update.Error
			if update.Error != nil {
				tx.Error = tx.AddError(update.Error)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	result, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if revokedFailure != nil || revokedRows != 1 {
		t.Fatal("revocation injection did not update exactly its real session", revokedFailure, revokedRows)
	}
	if !errors.Is(err, domain.ErrUnauthenticated) || result.ID != 0 {
		t.Fatal("commit-time revocation was not classified as unauthenticated", err)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	var session commonmodelv1.AdminSession
	if err := db.WithContext(ctx).Model(&commonmodelv1.AdminSession{}).Where("id = ?", admin.Principal.SessionID).Take(&session).Error; err != nil || session.RevokedAt != nil {
		t.Fatal("failed acceptance did not roll back the injected revocation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
	assertFlatAuditCount(ctx, t, db, 0)
	if _, err := store.Request(ctx, admin.Token, admin.CSRF, request); err != nil {
		t.Fatal("original authority and key were not restored by rollback", err)
	}
}
