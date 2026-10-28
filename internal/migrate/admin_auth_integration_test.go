//go:build integration

package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

const adminFixturePassword = "test-only-correct-password-27"

func TestAdminSessionsStoreDigestsAndEnforceRoleCsrfAndLogout(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator, domain.AdminRoleAdmin} {
		user, err := adminProvisionRequest(ctx, t, auth, string(role), role)
		if err != nil {
			t.Fatal(err)
		}
		login, err := adminLoginRequest(ctx, t, auth, string(role), adminFixturePassword)
		if err != nil || login.Principal.UserID != user.UserID || login.Principal.Role != role || len(login.Token) != 43 || len(login.CSRF) != 43 || login.Token == login.CSRF {
			t.Fatalf("login did not return a typed role-bound random session: role=%s error=%v scenario_context=%v", role, err, ctx.Err())
		}
		stored := assertAdminSessionStorage(ctx, t, db, user, login)
		for _, required := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator, domain.AdminRoleAdmin} {
			_, err := auth.Authorize(ctx, login.Token, login.CSRF, required)
			if role.Allows(required) && err != nil || !role.Allows(required) && !errors.Is(err, domain.ErrForbidden) {
				t.Fatal("management role boundary changed")
			}
		}
		if _, err := auth.Authorize(ctx, login.Token, "wrong-csrf", domain.AdminRoleViewer); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("write authorization ignored CSRF")
		}
		if err := auth.Logout(ctx, login.Token, login.CSRF); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.Identity(ctx, login.Token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("logged-out session remained authenticated")
		}
		if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", stored.Id).UpdateColumn("revoked_at", nil).Error; err == nil {
			t.Fatal("revoked session was resurrected")
		}
	}
	var audits []*modelv1.AdminAudit
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Order("id ASC").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 9 || audits[0].Id <= 0 || audits[0].CreateAt <= 0 || audits[0].UpdateAt <= 0 || audits[0].DeleteAt != nil {
		t.Fatal("identity/session audit transaction or common fields missing")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", audits[0].Id).UpdateColumn("reason", "rewritten").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", audits[0].Id).Delete(&modelv1.AdminAudit{}).Error, "23514")
}

func TestAdminFailedLoginsPersistAndCredentialChangesCloseExistingSessions(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	user, err := adminProvisionRequest(ctx, t, auth, "bounded-user", domain.AdminRoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	login, err := adminLoginRequest(ctx, t, auth, user.Username, adminFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	for range domain.MaxAdminLoginFailures {
		if _, err := adminLoginRequest(ctx, t, auth, user.Username, "test-only-wrong-password"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("wrong password changed the public rejection")
		}
	}
	if _, err := adminLoginRequest(ctx, t, auth, user.Username, adminFixturePassword); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("locked identity did not return the required rejection: error=%v scenario_context=%v", err, ctx.Err())
	}
	var row modelv1.AdminUser
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).First(&row, user.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if row.FailedAttempts != domain.MaxAdminLoginFailures || row.LockedUntil <= 0 {
		t.Fatal("failed login counters rolled back with the rejection")
	}
	if _, err := adminLoginRequest(ctx, t, auth, "unknown-user", adminFixturePassword); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("missing identity leaked a different rejection")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", row.Id).UpdateColumn("enabled", false).Error, "23514")
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", row.Id).
		Select("Enabled", "CredentialVersion", "Revision").Updates(&modelv1.AdminUser{
		Enabled:           false,
		CredentialVersion: row.CredentialVersion + 1,
		Revision:          row.Revision + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Identity(ctx, login.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("credential change or disable left old authority open")
	}
}

func TestAdminSessionAndAuditRollbackTogether(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminProvisionRequest(ctx, t, auth, "audit-user", domain.AdminRoleViewer); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("controlled audit insertion failure")
	const callback = "test:admin-audit-failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminAudit); ok {
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
	if login, err := adminLoginRequest(ctx, t, auth, "audit-user", adminFixturePassword); err == nil || login.Token != "" {
		t.Fatal("login succeeded without its durable audit")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed audit left a session")
	}
}

func TestAdminSessionExpiryAndActiveSessionLimit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	user, err := adminProvisionRequest(ctx, t, auth, "session-user", domain.AdminRoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", user.UserID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	// An explicitly historical test session validates DB-clock expiry without
	// changing a real token's immutable lifetime or sleeping for eight hours.
	expiredToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	digest := sha256.Sum256([]byte(expiredToken))
	old := &modelv1.AdminSession{
		CreateAt:          clock.NowMS - 1000,
		UpdateAt:          clock.NowMS - 1000,
		UserId:            user.UserID,
		TokenHash:         hex.EncodeToString(digest[:]),
		CsrfHash:          hex.EncodeToString(digest[:]),
		CredentialVersion: 1,
		ExpiresAt:         clock.NowMS - 1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Create(old).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Identity(ctx, expiredToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("expired token remained valid")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ?", old.Id).UpdateColumn("expires_at", clock.NowMS+1000).Error, "23514")
	var first, latest domain.AdminLogin
	for i := int64(0); i <= domain.MaxAdminSessions; i++ {
		latest, err = adminLoginRequest(ctx, t, auth, user.Username, adminFixturePassword)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = latest
		}
	}
	if _, err := auth.Identity(ctx, first.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("oldest session was not retired at the limit")
	}
	if _, err := auth.Identity(ctx, latest.Token); err != nil {
		t.Fatal("new session was not usable")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", user.UserID, clock.NowMS).Count(&count).Error; err != nil || count != domain.MaxAdminSessions {
		t.Fatal("active sessions exceeded the bound or counted expired history")
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("action = ?", domain.AdminAuditActionSessionEvicted).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("session retirement was not audited atomically")
	}
}

// Each independent cost-12 request keeps its own tighter budget. The scenario
// bound includes many sequential instrumented hashes, migrations and assertions.
func adminLoginRequest(ctx context.Context, t *testing.T, auth domain.AdminAuth, username, password string) (domain.AdminLogin, error) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := auth.Login(call, username, password)
	t.Logf("authentication request elapsed_ms=%d context_error=%v error=%v", time.Since(started).Milliseconds(), call.Err(), err)
	return result, err
}

func adminProvisionRequest(ctx context.Context, t *testing.T, auth domain.AdminAuth, username string, role domain.AdminRole) (domain.AdminPrincipal, error) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return auth.Provision(call, "local-provisioner", username, adminFixturePassword, role)
}

func assertAdminSessionStorage(ctx context.Context, t *testing.T, db *gorm.DB, user domain.AdminPrincipal, login domain.AdminLogin) *modelv1.AdminSession {
	t.Helper()
	var stored modelv1.AdminSession
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).First(&stored, login.Principal.SessionID).Error; err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(login.Token))
	csrf := sha256.Sum256([]byte(login.CSRF))
	if stored.TokenHash != hex.EncodeToString(digest[:]) || stored.CsrfHash != hex.EncodeToString(csrf[:]) || stored.TokenHash == login.Token {
		t.Fatal("raw session secrets persisted")
	}
	var identity modelv1.AdminUser
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).First(&identity, user.UserID).Error; err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(identity.PasswordHash))
	if err != nil || cost != 12 || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(adminFixturePassword)) != nil {
		t.Fatal("password did not use the fixed adaptive hash")
	}
	return &stored
}
