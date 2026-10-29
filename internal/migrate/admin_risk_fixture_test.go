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

func riskActorFixture(ctx context.Context, t *testing.T, db *gorm.DB, name string, role domain.AdminRole, seed byte, ttl time.Duration) domain.AdminLogin {
	t.Helper()
	user := &modelv1.AdminUser{
		Username:          name,
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(role),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(user).Error; err != nil {
		t.Fatal(err)
	}
	now := controlTestDatabaseTime(ctx, t, db, user.Id)
	token, hash := controlTestSecret(seed)
	csrf, csrfHash := controlTestSecret(seed + 1)
	session := &modelv1.AdminSession{
		CreateAt:          now,
		UpdateAt:          now,
		UserId:            user.Id,
		TokenHash:         hash,
		CsrfHash:          csrfHash,
		CredentialVersion: 1,
		ExpiresAt:         now + ttl.Milliseconds(),
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Create(session).Error; err != nil {
		t.Fatal(err)
	}
	return domain.AdminLogin{
		Principal: domain.AdminPrincipal{
			UserID:    user.Id,
			Username:  name,
			Role:      role,
			SessionID: session.Id,
			ExpiresAt: time.UnixMilli(session.ExpiresAt).UTC(),
		},
		Token: token,
		CSRF:  csrf,
	}
}

func riskBlockRequest(id int64) domain.AdminRiskRequest {
	return domain.AdminRiskRequest{
		Key:              "risk-managed-strategy-block-0001",
		StrategyID:       id,
		ExpectedRevision: 1,
		Action:           domain.AdminRiskBlock,
		Reason:           "explicit emergency strategy risk block",
	}
}

func riskRestoreRequest(instance *modelv1.StrategyInstance) domain.AdminRiskRequest {
	return domain.AdminRiskRequest{
		Key:                   "risk-managed-strategy-unblock-0001",
		StrategyID:            instance.Id,
		ExpectedRevision:      2,
		ExpectedConfigVersion: instance.RequestedConfigVersion,
		ExpectedDesiredState:  domain.StrategyState(instance.DesiredState),
		Action:                domain.AdminRiskUnblock,
		Reason:                "administrator confirms fresh recovery checks",
	}
}

func riskControlFixture(t *testing.T, db *gorm.DB) domain.AdminStrategyRisk {
	t.Helper()
	control, err := repository.NewAdminStrategyRisk(db)
	if err != nil {
		t.Fatal(err)
	}
	return control
}

func refreshRiskSelectedMarket(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64) {
	t.Helper()
	feed, token, snapshots, current := admissionSelectedMarket(ctx, t, db, instrumentID)
	reading := marketReadingFixture(feed, current.Reading.Sequence+1, "100")
	reading.ValidUntil = reading.ReceivedAt.Add(4 * time.Second)
	if _, err := snapshots.Publish(ctx, token, current.Revision, reading); err != nil {
		t.Fatal(err)
	}
}

func assertRiskControlState(ctx context.Context, t *testing.T, control domain.AdminStrategyRisk, actor domain.AdminLogin, id int64, enabled bool, revision int64) {
	t.Helper()
	state, err := control.State(ctx, actor.Token, id)
	if err != nil || state.Enabled != enabled || state.Revision != revision {
		t.Fatalf("risk enabled=%t revision=%d, expected enabled=%t revision=%d: %v", state.Enabled, state.Revision, enabled, revision, err)
	}
}
