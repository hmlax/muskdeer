package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminStrategyRisk struct {
	db        *gorm.DB
	snapshots adminStrategies
}

// NewAdminStrategyRisk borrows existing storage without granting or starting work.
func NewAdminStrategyRisk(db *gorm.DB) (domain.AdminStrategyRisk, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminRisk
	}
	return &adminStrategyRisk{
		db:        db,
		snapshots: adminStrategies{db: db},
	}, nil
}

func (s *adminStrategyRisk) Change(ctx context.Context, token, csrf string, request domain.AdminRiskRequest) (domain.AdminRiskReceipt, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	var result domain.AdminRiskReceipt
	err = s.db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = changeAdminRisk(ctx, tx, token, csrf, request)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminRiskReceipt{}, adminRiskFailure(err)
	}
	return result, nil
}

func changeAdminRisk(ctx context.Context, tx *gorm.DB, token, csrf string, request domain.AdminRiskRequest) (domain.AdminRiskReceipt, error) {
	user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
	if err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	if request.Action == domain.AdminRiskUnblock && !domain.AdminRole(user.Role).Allows(domain.AdminRoleAdmin) {
		return domain.AdminRiskReceipt{}, domain.ErrForbidden
	}
	fingerprint := adminRiskFingerprint(request)
	var previous modelv1.AdminStrategyRiskChange
	err = tx.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).
		Where("actor_user_id = ? AND idempotency_key = ?", user.Id, request.Key).Take(&previous).Error
	if err == nil {
		if previous.RequestFingerprint != fingerprint {
			return domain.AdminRiskReceipt{}, domain.ErrAdminConflict
		}
		return adminRiskReceipt(&previous), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.AdminRiskReceipt{}, err
	}
	var instance modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", request.StrategyID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
		return domain.AdminRiskReceipt{}, adminRiskLookupFailure(err)
	}
	revision, err := adminRiskRevision(ctx, tx, instance.Id)
	if err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	if revision != request.ExpectedRevision || request.Action == domain.AdminRiskUnblock &&
		(instance.RequestedConfigVersion != request.ExpectedConfigVersion || instance.DesiredState != string(request.ExpectedDesiredState)) {
		return domain.AdminRiskReceipt{}, domain.ErrAdminConflict
	}
	budget, instrument, err := lockAdminRiskBudget(ctx, tx, &instance)
	if err != nil {
		return domain.AdminRiskReceipt{}, adminRiskLookupFailure(err)
	}
	wasEnabled := budget.Enabled
	budget.Enabled = request.Action == domain.AdminRiskUnblock
	budget.Revision++
	if err := reservationResult(tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("id = ? AND revision = ?", budget.Id, budget.Revision-1).
		Select("Enabled", "Revision").Updates(budget)); err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	var recoveryUntil *int64
	if budget.Enabled {
		until, err := validateAdminRiskRecovery(ctx, tx, &instance, budget, instrument)
		if err != nil {
			return domain.AdminRiskReceipt{}, err
		}
		recoveryUntil = proto.Int64(until.UnixMilli())
	}
	if err := invalidateLifecycleRisk(ctx, tx, &instance); err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	return recordAdminRisk(ctx, tx, user, session, &instance, request, fingerprint, wasEnabled, recoveryUntil)
}

func lockAdminRiskBudget(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance) (*modelv1.StrategyBudget, *modelv1.Instrument, error) {
	// Keep the execution order even for block: instrument -> core account ->
	// account budget -> strategy budget. Do not reverse shared financial locks.
	rows, err := lockCoreRows(ctx, tx, instance.AccountId, instance.InstrumentId)
	if err != nil {
		return nil, nil, err
	}
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", instance.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&account).Error; err != nil {
		return nil, nil, err
	}
	var budget modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", instance.Id, instance.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&budget).Error; err != nil {
		return nil, nil, err
	}
	return &budget, rows.instrument, nil
}

func adminRiskRevision(ctx context.Context, tx *gorm.DB, strategyID int64) (int64, error) {
	var result struct{ Revision int64 }
	err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Where("strategy_instance_id = ?", strategyID).
		Select("COALESCE(max(revision), 1)::bigint AS revision").Take(&result).Error
	return result.Revision, err
}

func recordAdminRisk(ctx context.Context, tx *gorm.DB, user *modelv1.AdminUser, session *modelv1.AdminSession, instance *modelv1.StrategyInstance, request domain.AdminRiskRequest, fingerprint string, previousEnabled bool, recoveryUntil *int64) (domain.AdminRiskReceipt, error) {
	audit := &modelv1.AdminAudit{
		Actor:         user.Username,
		ActorRole:     user.Role,
		Action:        string(request.Action),
		Resource:      domain.AdminAuditResourceRisk + ":" + strconv.FormatInt(instance.Id, 10),
		Reason:        request.Reason,
		BeforeVersion: proto.Int64(request.ExpectedRevision),
		AfterVersion:  proto.Int64(request.ExpectedRevision + 1),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	row := &modelv1.AdminStrategyRiskChange{
		ActorUserId:        user.Id,
		ActorSessionId:     session.Id,
		StrategyInstanceId: instance.Id,
		AuditId:            audit.Id,
		IdempotencyKey:     request.Key,
		RequestFingerprint: fingerprint,
		Action:             string(request.Action),
		PreviousEnabled:    previousEnabled,
		Enabled:            request.Action == domain.AdminRiskUnblock,
		PreviousRevision:   request.ExpectedRevision,
		Revision:           request.ExpectedRevision + 1,
		Reason:             request.Reason,
		RecoveryUntil:      recoveryUntil,
	}
	if row.Enabled {
		row.ExpectedConfigVersion = proto.Int64(request.ExpectedConfigVersion)
		row.ExpectedDesiredState = proto.String(string(request.ExpectedDesiredState))
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Create(row).Error; err != nil {
		return domain.AdminRiskReceipt{}, err
	}
	return adminRiskReceipt(row), nil
}

func adminRiskFingerprint(request domain.AdminRiskRequest) string {
	data := binary.BigEndian.AppendUint64(nil, uint64(request.StrategyID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedRevision))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedConfigVersion))
	for _, field := range []string{string(request.Action), string(request.ExpectedDesiredState), request.Reason} {
		data = binary.BigEndian.AppendUint32(data, uint32(len(field)))
		data = append(data, field...)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func adminRiskReceipt(row *modelv1.AdminStrategyRiskChange) domain.AdminRiskReceipt {
	return domain.AdminRiskReceipt{
		ID:                    row.Id,
		ActorUserID:           row.ActorUserId,
		StrategyID:            row.StrategyInstanceId,
		Action:                domain.AdminRiskAction(row.Action),
		PreviousEnabled:       row.PreviousEnabled,
		Enabled:               row.Enabled,
		PreviousRevision:      row.PreviousRevision,
		Revision:              row.Revision,
		ExpectedConfigVersion: row.GetExpectedConfigVersion(),
		ExpectedDesiredState:  domain.StrategyState(row.GetExpectedDesiredState()),
		Reason:                row.Reason,
		CreatedAt:             time.UnixMilli(row.CreateAt).UTC(),
	}
}

func adminRiskLookupFailure(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrAdminNotFound
	}
	return err
}

func adminRiskFailure(err error) error {
	for _, known := range []error{domain.ErrInvalidAdminRisk, domain.ErrAdminRiskRecoveryDenied, domain.ErrUnauthenticated, domain.ErrForbidden, domain.ErrAdminConflict, domain.ErrAdminNotFound, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return err
		}
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.ConstraintName {
		case "admin_risk_authority":
			return domain.ErrUnauthenticated
		case "admin_risk_recovery":
			return domain.ErrAdminRiskRecoveryDenied
		}
	}
	return &adminControlError{cause: err}
}

var _ domain.AdminStrategyRisk = (*adminStrategyRisk)(nil)
