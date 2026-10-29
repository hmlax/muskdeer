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

func TestAdminRiskSessionExpiringAfterReceiptCannotCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	short := riskActorFixture(ctx, t, db, "risk-expiring-operator", domain.AdminRoleOperator, 61, time.Second)
	control := riskControlFixture(t, db)
	const callback = "test:expire-risk-authority-after-receipt"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminStrategyRiskChange); ok && tx.Error == nil {
			waitRiskDatabaseExpiry(ctx, t, db, operator.Principal.UserID, short.Principal.ExpiresAt.UnixMilli())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	id := submit.Input.Authority.StrategyID
	result, err := control.Change(ctx, short.Token, short.CSRF, riskBlockRequest(id))
	if !errors.Is(err, domain.ErrUnauthenticated) || result.ID != 0 {
		t.Fatal("session expired after receipt insertion still committed", err)
	}
	assertRiskControlState(ctx, t, control, operator, id, true, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 0)
}

func TestAdminRiskRecoveryExpiringAfterReceiptCannotCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	instance := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	if _, err := control.Change(ctx, operator.Token, operator.CSRF, riskBlockRequest(instance.Id)); err != nil {
		t.Fatal(err)
	}
	const callback = "test:expire-risk-observation-after-receipt"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*modelv1.AdminStrategyRiskChange); ok && tx.Error == nil && row.Enabled {
			if row.RecoveryUntil == nil {
				t.Error("recovery receipt omitted its bounded proof")
				return
			}
			waitRiskDatabaseExpiry(ctx, t, db, operator.Principal.UserID, *row.RecoveryUntil)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	result, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(instance))
	if !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) || result.ID != 0 {
		t.Fatal("expired recovery observation committed an enabled flag", err)
	}
	assertRiskControlState(ctx, t, control, operator, instance.Id, false, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
}

func waitRiskDatabaseExpiry(ctx context.Context, t *testing.T, db *gorm.DB, userID, deadline int64) {
	t.Helper()
	ticks := time.NewTicker(5 * time.Millisecond)
	defer ticks.Stop()
	for controlTestDatabaseTime(ctx, t, db, userID) <= deadline {
		select {
		case <-ticks.C:
		case <-ctx.Done():
			t.Fatal("risk expiry proof exceeded its original operation budget", ctx.Err())
		}
	}
}
