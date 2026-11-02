package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminAccountFlattenings struct{ db *gorm.DB }

// NewAdminAccountFlattenings borrows one database without resources or work.
func NewAdminAccountFlattenings(db *gorm.DB) (domain.AdminAccountFlattenings, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminFlatten
	}
	return &adminAccountFlattenings{db: db}, nil
}

func (s *adminAccountFlattenings) Request(ctx context.Context, token, csrf string, request domain.AdminFlattenRequest) (domain.AdminFlattenReceipt, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	var result domain.AdminFlattenReceipt
	err = s.db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = requestAdminFlattening(ctx, tx, token, csrf, request)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminFlattenReceipt{}, adminFlattenFailure(err)
	}
	return result, nil
}

func (s *adminAccountFlattenings) Confirmation(ctx context.Context, token string, strategyID, id int64) (domain.AdminFlattenReceipt, error) {
	if strategyID <= 0 || id <= 0 {
		return domain.AdminFlattenReceipt{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminFlattenReceipt
	snapshots := adminStrategies{db: s.db}
	err := snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.AdminAccountFlattening
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).
			Where("id = ? AND strategy_instance_id = ? AND delete_at IS NULL", id, strategyID).Take(&row).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		var err error
		result, err = adminFlattenReceipt(&row)
		return err
	})
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	return result, nil
}

func requestAdminFlattening(ctx context.Context, tx *gorm.DB, token, csrf string, request domain.AdminFlattenRequest) (domain.AdminFlattenReceipt, error) {
	user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	if user.Role != string(domain.AdminRoleAdmin) {
		return domain.AdminFlattenReceipt{}, domain.ErrForbidden
	}
	fingerprint := adminFlattenFingerprint(request)
	previous, found, err := previousAdminFlattening(ctx, tx, user.Id, request.Key, fingerprint)
	if err != nil || found {
		return previous, err
	}
	var instance modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", request.StrategyID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
		return domain.AdminFlattenReceipt{}, adminRiskLookupFailure(err)
	}
	if instance.AccountId != request.AccountID || instance.InstrumentId != request.InstrumentID {
		return domain.AdminFlattenReceipt{}, domain.ErrInvalidAdminFlatten
	}
	core, err := lockCoreRows(ctx, tx, request.AccountID, request.InstrumentID)
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", request.AccountID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Take(&account).Error; err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	// Re-read under the financial mutex before considering a pending account.
	previous, found, err = previousAdminFlattening(ctx, tx, user.Id, request.Key, fingerprint)
	if err != nil || found {
		return previous, err
	}
	var pending int64
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("account_id = ? AND delete_at IS NULL", request.AccountID).Count(&pending).Error; err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	if pending != 0 {
		return domain.AdminFlattenReceipt{}, domain.ErrAdminConflict
	}
	basis, err := adminFlattenBasis(ctx, tx, &instance, core, &account)
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	if basis.RiskRevision != request.ExpectedRiskRevision || basis.PortfolioRevision != request.ExpectedPortfolioRevision || basis.BaseRevision != request.ExpectedBaseRevision ||
		!basis.CoreAvailable.Equal(request.ConfirmedQuantity) {
		return domain.AdminFlattenReceipt{}, domain.ErrAdminConflict
	}
	basis.Cancellation, err = flattenCancellationProof(ctx, tx, request.StrategyID, request.CancellationID)
	if err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	if _, err := domain.PlanAdminFlatten(request, basis); err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	return recordAdminFlattening(ctx, tx, user, session, request, fingerprint)
}

func previousAdminFlattening(ctx context.Context, tx *gorm.DB, actorID int64, key, fingerprint string) (domain.AdminFlattenReceipt, bool, error) {
	var row modelv1.AdminAccountFlattening
	err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("actor_user_id = ? AND idempotency_key = ?", actorID, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.AdminFlattenReceipt{}, false, nil
	}
	if err != nil {
		return domain.AdminFlattenReceipt{}, false, err
	}
	if row.RequestFingerprint != fingerprint {
		return domain.AdminFlattenReceipt{}, false, domain.ErrAdminConflict
	}
	result, err := adminFlattenReceipt(&row)
	return result, true, err
}

func recordAdminFlattening(ctx context.Context, tx *gorm.DB, user *modelv1.AdminUser, session *modelv1.AdminSession, request domain.AdminFlattenRequest, fingerprint string) (domain.AdminFlattenReceipt, error) {
	audit := &modelv1.AdminAudit{
		Actor:         user.Username,
		ActorRole:     user.Role,
		Action:        domain.AdminFlattenAction,
		Resource:      domain.AdminFlattenAuditResource + ":" + request.AccountID + ":" + strconv.FormatInt(request.InstrumentID, 10),
		Reason:        request.Reason,
		BeforeVersion: proto.Int64(request.ExpectedPortfolioRevision),
		AfterVersion:  proto.Int64(request.ExpectedPortfolioRevision),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	row := &modelv1.AdminAccountFlattening{
		ActorUserId:               user.Id,
		ActorSessionId:            session.Id,
		StrategyInstanceId:        request.StrategyID,
		AccountId:                 request.AccountID,
		InstrumentId:              request.InstrumentID,
		EmergencyCancellationId:   request.CancellationID,
		AuditId:                   audit.Id,
		IdempotencyKey:            request.Key,
		RequestFingerprint:        fingerprint,
		Scope:                     string(request.Scope),
		ExpectedRiskRevision:      request.ExpectedRiskRevision,
		ExpectedPortfolioRevision: request.ExpectedPortfolioRevision,
		ExpectedBaseRevision:      request.ExpectedBaseRevision,
		ConfirmedQuantity:         request.ConfirmedQuantity.String(),
		LimitPrice:                request.LimitPrice.String(),
		Reason:                    request.Reason,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(row).Error; err != nil {
		return domain.AdminFlattenReceipt{}, err
	}
	return adminFlattenReceipt(row)
}

func adminFlattenFailure(err error) error {
	for _, known := range []error{domain.ErrInvalidAdminFlatten, domain.ErrAdminFlattenDenied} {
		if errors.Is(err, known) {
			return err
		}
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch {
		case databaseError.ConstraintName == "admin_flatten_authority":
			return domain.ErrUnauthenticated
		case databaseError.Code == "23505", databaseError.ConstraintName == "admin_flatten_pending", databaseError.ConstraintName == "admin_flatten_confirmation":
			return domain.ErrAdminConflict
		case databaseError.ConstraintName == "admin_flatten_scope":
			return domain.ErrAdminFlattenDenied
		}
	}
	return adminControlFailure(err)
}

var _ domain.AdminAccountFlattenings = (*adminAccountFlattenings)(nil)
