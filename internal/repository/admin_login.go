package repository

import (
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (a *adminAuth) completeLogin(ctx context.Context, candidate *modelv1.AdminUser, matched bool) (domain.AdminLogin, error) {
	var result domain.AdminLogin
	denied := true
	err := a.db.WithContext(ctx).Model(&modelv1.AdminUser{}).Transaction(func(tx *gorm.DB) error {
		var user modelv1.AdminUser
		if err := tx.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ? AND delete_at IS NULL", candidate.Id).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&user).Error; err != nil {
			return err
		}
		now, err := adminDatabaseTime(ctx, tx, user.Id)
		if err != nil {
			return err
		}
		if !user.Enabled || user.LockedUntil > now || user.CredentialVersion != candidate.CredentialVersion || user.PasswordHash != candidate.PasswordHash {
			return nil
		}
		if user.LockedUntil > 0 {
			user.FailedAttempts, user.LockedUntil = 0, 0
		}
		if !matched {
			user.FailedAttempts++
			if user.FailedAttempts >= domain.MaxAdminLoginFailures {
				user.LockedUntil = now + domain.AdminLockDuration.Milliseconds()
			}
			if err := updateAdminFailures(ctx, tx, &user); err != nil {
				return err
			}
			return writeAdminAudit(ctx, tx, user.Username, domain.AdminRole(user.Role), domain.AdminAuditActionLoginDenied, domain.AdminAuditResourceUser, user.Id, domain.AdminAuditReasonDenied)
		}
		user.FailedAttempts, user.LockedUntil = 0, 0
		if err := updateAdminFailures(ctx, tx, &user); err != nil {
			return err
		}
		result, err = createAdminSession(ctx, tx, &user, now)
		if err != nil {
			return err
		}
		denied = false
		return writeAdminAudit(ctx, tx, user.Username, domain.AdminRole(user.Role), domain.AdminAuditActionLogin, domain.AdminAuditResourceSession, result.Principal.SessionID, domain.AdminAuditReasonLogin)
	})
	if err != nil {
		return domain.AdminLogin{}, adminAuthFailure(err)
	}
	if denied {
		return domain.AdminLogin{}, domain.ErrUnauthenticated
	}
	return result, nil
}

func updateAdminFailures(ctx context.Context, tx *gorm.DB, row *modelv1.AdminUser) error {
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("FailedAttempts", "LockedUntil", "Revision").Updates(row)
	return reservationResult(result)
}

func createAdminSession(ctx context.Context, tx *gorm.DB, user *modelv1.AdminUser, now int64) (domain.AdminLogin, error) {
	var result domain.AdminLogin
	if err := makeAdminSessionRoom(ctx, tx, user, now); err != nil {
		return result, err
	}
	token, err := adminSecret()
	if err != nil {
		return result, err
	}
	csrf, err := adminSecret()
	if err != nil {
		return result, err
	}
	tokenHash, valid := adminSecretHash(token)
	csrfHash, csrfValid := adminSecretHash(csrf)
	if !valid || !csrfValid {
		return result, domain.ErrAuthUnavailable
	}
	row := &modelv1.AdminSession{
		UserId:            user.Id,
		TokenHash:         tokenHash,
		CsrfHash:          csrfHash,
		CredentialVersion: user.CredentialVersion,
		ExpiresAt:         now + domain.AdminSessionTTL.Milliseconds(),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Create(row).Error; err != nil {
		return result, err
	}
	return domain.AdminLogin{
		Principal: adminPrincipal(user, row),
		Token:     token,
		CSRF:      csrf,
	}, nil
}

func makeAdminSessionRoom(ctx context.Context, tx *gorm.DB, user *modelv1.AdminUser, now int64) error {
	userID := user.Id
	var count int64
	if err := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ? AND delete_at IS NULL", userID, now).Count(&count).Error; err != nil {
		return err
	}
	if count > domain.MaxAdminSessions {
		return domain.ErrAuthUnavailable
	}
	if count < domain.MaxAdminSessions {
		return nil
	}
	var oldest modelv1.AdminSession
	if err := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).
		Where("user_id = ? AND revoked_at IS NULL AND expires_at > ? AND delete_at IS NULL", userID, now).Order("id ASC").
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&oldest).Error; err != nil {
		return err
	}
	updated := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ? AND revoked_at IS NULL", oldest.Id).
		Select("RevokedAt", "UpdateAt").Updates(&modelv1.AdminSession{
		RevokedAt: &now,
		UpdateAt:  now,
	})
	if err := reservationResult(updated); err != nil {
		return err
	}
	return writeAdminAudit(ctx, tx, user.Username, domain.AdminRole(user.Role), domain.AdminAuditActionSessionEvicted, domain.AdminAuditResourceSession, oldest.Id, domain.AdminAuditReasonEvicted)
}

func adminPrincipal(user *modelv1.AdminUser, session *modelv1.AdminSession) domain.AdminPrincipal {
	return domain.AdminPrincipal{
		UserID:    user.Id,
		Username:  user.Username,
		Role:      domain.AdminRole(user.Role),
		SessionID: session.Id,
		ExpiresAt: time.UnixMilli(session.ExpiresAt).UTC(),
	}
}

func adminDatabaseTime(ctx context.Context, tx *gorm.DB, userID int64) (int64, error) {
	var clock struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", userID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
	return clock.NowMS, err
}

func writeAdminAudit(ctx context.Context, tx *gorm.DB, actor string, role domain.AdminRole, action, resource string, resourceID int64, reason string) error {
	return tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(&modelv1.AdminAudit{
		Actor:     actor,
		ActorRole: string(role),
		Action:    action,
		Resource:  resource + ":" + strconv.FormatInt(resourceID, 10),
		Reason:    reason,
	}).Error
}
