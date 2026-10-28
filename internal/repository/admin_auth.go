package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const (
	adminPasswordCost    = 12
	adminHashConcurrency = 4
	// A public, non-identity dummy hash keeps missing-user verification at the
	// same cost. It cannot authenticate any account or create a session.
	adminDummyHash = "$2a$12$UyD8sD3uBc9OgPPOPaHNHefnzpTxBqkZxOZWqyTD0sPNkSl89DQOa"
)

type adminAuth struct {
	db        *gorm.DB
	hashSlots chan struct{}
}

type adminAuthPersistenceError struct{ cause error }

func (*adminAuthPersistenceError) Error() string { return domain.MessageAuthUnavailable }
func (e *adminAuthPersistenceError) Unwrap() error {
	return errors.Join(domain.ErrAuthUnavailable, e.cause)
}

// NewAdminAuth borrows storage; it creates no identity, task or permission.
func NewAdminAuth(db *gorm.DB) (domain.AdminAuth, error) {
	if db == nil {
		return nil, domain.ErrInvalidAuth
	}
	return &adminAuth{
		db:        db,
		hashSlots: make(chan struct{}, adminHashConcurrency),
	}, nil
}

func (a *adminAuth) Provision(ctx context.Context, actor, username, password string, role domain.AdminRole) (domain.AdminPrincipal, error) {
	name, valid := adminUsername(username)
	actor, actorValid := adminUsername(actor)
	if !valid || !actorValid || !adminPassword(password) || !role.Allows(role) {
		return domain.AdminPrincipal{}, domain.ErrInvalidAuth
	}
	if err := a.acquireHash(ctx); err != nil {
		return domain.AdminPrincipal{}, err
	}
	defer a.releaseHash()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), adminPasswordCost)
	if err != nil {
		return domain.AdminPrincipal{}, adminAuthFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return domain.AdminPrincipal{}, err
	}
	row := &modelv1.AdminUser{
		Username:          name,
		PasswordHash:      string(hash),
		Role:              string(role),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}
	err = a.db.WithContext(ctx).Model(&modelv1.AdminUser{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(row).Error; err != nil {
			return err
		}
		return writeAdminAudit(ctx, tx, actor, domain.AdminRoleAdmin, domain.AdminAuditActionProvision, domain.AdminAuditResourceUser, row.Id, domain.AdminAuditReasonProvision)
	})
	if err != nil {
		return domain.AdminPrincipal{}, adminAuthFailure(err)
	}
	return domain.AdminPrincipal{
		UserID:   row.Id,
		Username: row.Username,
		Role:     role,
	}, nil
}

func (a *adminAuth) Login(ctx context.Context, username, password string) (domain.AdminLogin, error) {
	name, valid := adminUsername(username)
	if !valid || !adminPassword(password) {
		return domain.AdminLogin{}, domain.ErrUnauthenticated
	}
	if err := a.acquireHash(ctx); err != nil {
		return domain.AdminLogin{}, err
	}
	defer a.releaseHash()
	var candidate modelv1.AdminUser
	err := a.db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("username = ? AND delete_at IS NULL", name).Take(&candidate).Error
	found := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.AdminLogin{}, adminAuthFailure(err)
	}
	hash := adminDummyHash
	if found {
		hash = candidate.PasswordHash
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != adminPasswordCost {
		return domain.AdminLogin{}, domain.ErrAuthUnavailable
	}
	comparison := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err := ctx.Err(); err != nil {
		return domain.AdminLogin{}, err
	}
	if comparison != nil && !errors.Is(comparison, bcrypt.ErrMismatchedHashAndPassword) {
		return domain.AdminLogin{}, domain.ErrAuthUnavailable
	}
	if !found {
		return domain.AdminLogin{}, domain.ErrUnauthenticated
	}
	return a.completeLogin(ctx, &candidate, comparison == nil)
}

func (a *adminAuth) acquireHash(ctx context.Context) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	select {
	case a.hashSlots <- struct{}{}:
		return nil
	default:
		return domain.ErrAuthUnavailable
	}
}

func (a *adminAuth) releaseHash() { <-a.hashSlots }

func adminUsername(value string) (string, bool) {
	value = strings.ToLower(value)
	if len(value) < 3 || len(value) > 64 {
		return "", false
	}
	for i := range len(value) {
		character := value[i]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if i > 0 && (character == '_' || character == '-' || character == '.') {
			continue
		}
		return "", false
	}
	return value, true
}

func adminPassword(value string) bool {
	return len(value) >= 12 && len(value) <= 72 && utf8.ValidString(value)
}

func adminSecret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func adminSecretHash(value string) (string, bool) {
	if len(value) != 43 {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return "", false
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:]), true
}

func adminAuthFailure(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{domain.ErrUnauthenticated, domain.ErrForbidden, domain.ErrInvalidAuth, domain.ErrAuthUnavailable, context.DeadlineExceeded, context.Canceled} {
		if errors.Is(err, known) {
			return err
		}
	}
	return &adminAuthPersistenceError{cause: err}
}

var _ domain.AdminAuth = (*adminAuth)(nil)
