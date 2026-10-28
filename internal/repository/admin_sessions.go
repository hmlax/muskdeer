package repository

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (a *adminAuth) Identity(ctx context.Context, token string) (domain.AdminPrincipal, error) {
	return a.readPrincipal(ctx, token, "", domain.AdminRoleViewer)
}

func (a *adminAuth) Authorize(ctx context.Context, token, csrf string, required domain.AdminRole) (domain.AdminPrincipal, error) {
	if _, valid := adminSecretHash(csrf); !valid {
		return domain.AdminPrincipal{}, domain.ErrForbidden
	}
	return a.readPrincipal(ctx, token, csrf, required)
}

func (a *adminAuth) readPrincipal(ctx context.Context, token, csrf string, required domain.AdminRole) (domain.AdminPrincipal, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminPrincipal{}, err
	}
	var result domain.AdminPrincipal
	err := a.db.WithContext(ctx).Model(&modelv1.AdminSession{}).Transaction(func(tx *gorm.DB) error {
		user, session, err := readAdminAuthority(ctx, tx, token, csrf, required, "")
		if err != nil {
			return err
		}
		result = adminPrincipal(user, session)
		return nil
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	return result, adminAuthFailure(err)
}

func (a *adminAuth) Logout(ctx context.Context, token, csrf string) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	if _, valid := adminSecretHash(csrf); !valid {
		return domain.ErrForbidden
	}
	err := a.db.WithContext(ctx).Model(&modelv1.AdminSession{}).Transaction(func(tx *gorm.DB) error {
		user, session, err := readAdminAuthority(ctx, tx, token, csrf, domain.AdminRoleViewer, "NO KEY UPDATE")
		if err != nil {
			return err
		}
		now, err := adminDatabaseTime(ctx, tx, user.Id)
		if err != nil {
			return err
		}
		updated := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("id = ? AND revoked_at IS NULL", session.Id).
			Select("RevokedAt", "UpdateAt").Updates(&modelv1.AdminSession{
			RevokedAt: &now,
			UpdateAt:  now,
		})
		if err := reservationResult(updated); err != nil {
			return err
		}
		return writeAdminAudit(ctx, tx, user.Username, domain.AdminRole(user.Role), domain.AdminAuditActionLogout, domain.AdminAuditResourceSession, session.Id, domain.AdminAuditReasonLogout)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return adminAuthFailure(err)
}

// With a lock strength, this helper belongs INSIDE the protected mutation's
// transaction: user SHARE first, then session. Role/credential changes and
// logout cannot commit across the effect. Read-only identity uses a RR snapshot.
func readAdminAuthority(ctx context.Context, tx *gorm.DB, token, csrf string, required domain.AdminRole, sessionStrength string) (*modelv1.AdminUser, *modelv1.AdminSession, error) {
	userStrength := ""
	if sessionStrength != "" {
		userStrength = "SHARE"
	}
	return readAdminAuthorityLocks(ctx, tx, token, csrf, required, userStrength, sessionStrength)
}

// Mutations serialize actor-scoped keys at the user row before any session or
// strategy lock. This also keeps credential change/logout outside the effect.
func readAdminControlAuthority(ctx context.Context, tx *gorm.DB, token, csrf string) (*modelv1.AdminUser, *modelv1.AdminSession, error) {
	if _, valid := adminSecretHash(csrf); !valid {
		return nil, nil, domain.ErrForbidden
	}
	return readAdminAuthorityLocks(ctx, tx, token, csrf, domain.AdminRoleOperator, "NO KEY UPDATE", "SHARE")
}

func readAdminAuthorityLocks(ctx context.Context, tx *gorm.DB, token, csrf string, required domain.AdminRole, userStrength, sessionStrength string) (*modelv1.AdminUser, *modelv1.AdminSession, error) {
	hash, valid := adminSecretHash(token)
	if !valid {
		return nil, nil, domain.ErrUnauthenticated
	}
	var first modelv1.AdminSession
	if err := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Where("token_hash = ? AND delete_at IS NULL", hash).Take(&first).Error; err != nil {
		return nil, nil, adminSessionLookupFailure(err)
	}
	var user modelv1.AdminUser
	userQuery := tx.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ? AND enabled AND delete_at IS NULL", first.UserId)
	if userStrength != "" {
		userQuery = userQuery.Clauses(clause.Locking{Strength: userStrength})
	}
	if err := userQuery.First(&user).Error; err != nil {
		return nil, nil, adminSessionLookupFailure(err)
	}
	var session modelv1.AdminSession
	sessionQuery := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).
		Where("id = ? AND token_hash = ? AND user_id = ? AND credential_version = ? AND revoked_at IS NULL AND delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint", first.Id, hash, user.Id, user.CredentialVersion)
	if sessionStrength != "" {
		sessionQuery = sessionQuery.Clauses(clause.Locking{Strength: sessionStrength})
	}
	if err := sessionQuery.First(&session).Error; err != nil {
		return nil, nil, adminSessionLookupFailure(err)
	}
	if !domain.AdminRole(user.Role).Allows(required) {
		return nil, nil, domain.ErrForbidden
	}
	if csrf != "" {
		proof, valid := adminSecretHash(csrf)
		if !valid || subtle.ConstantTimeCompare([]byte(proof), []byte(session.CsrfHash)) != 1 {
			return nil, nil, domain.ErrForbidden
		}
	}
	return &user, &session, nil
}

func adminSessionLookupFailure(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrUnauthenticated
	}
	return err
}
