//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Public test-only cost-12 bcrypt of adminFixturePassword. Native HTTP tests
// still perform real password login. Control transaction tests prepare current
// DB authority directly; original authentication tests retain their own bounds.
const controlFixturePasswordHash = "$2a$12$7tQm96yDmHbyy.svz42vLuGk.QfcJHSy6QhDGQ2OmP3LbOC9y16fO"

func adminControlSessionFixture(ctx context.Context, t *testing.T, db *gorm.DB) domain.AdminLogin {
	t.Helper()
	user := &modelv1.AdminUser{
		Username:          "control-operator",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleOperator),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(user).Error; err != nil {
		t.Fatal(err)
	}
	now := controlTestDatabaseTime(ctx, t, db, user.Id)
	token, tokenHash := controlTestSecret(41)
	csrf, csrfHash := controlTestSecret(42)
	session := &modelv1.AdminSession{
		CreateAt:          now,
		UpdateAt:          now,
		UserId:            user.Id,
		TokenHash:         tokenHash,
		CsrfHash:          csrfHash,
		CredentialVersion: user.CredentialVersion,
		ExpiresAt:         now + domain.AdminSessionTTL.Milliseconds(),
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Create(session).Error; err != nil {
		t.Fatal(err)
	}
	return domain.AdminLogin{
		Principal: domain.AdminPrincipal{
			UserID:    user.Id,
			Username:  user.Username,
			Role:      domain.AdminRoleOperator,
			SessionID: session.Id,
			ExpiresAt: time.UnixMilli(session.ExpiresAt).UTC(),
		},
		Token: token,
		CSRF:  csrf,
	}
}
