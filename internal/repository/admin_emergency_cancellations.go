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

type adminEmergencyCancellations struct{ db *gorm.DB }

// NewAdminEmergencyCancellations borrows one database, with no startup IO or work.
func NewAdminEmergencyCancellations(db *gorm.DB) (domain.AdminEmergencyCancellations, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	return &adminEmergencyCancellations{db: db}, nil
}

func (s *adminEmergencyCancellations) Request(ctx context.Context, token, csrf string, request domain.AdminEmergencyCancelRequest) (domain.AdminEmergencyCancelAcceptance, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	var result domain.AdminEmergencyCancelAcceptance
	err = s.db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = requestAdminEmergencyCancellation(ctx, tx, token, csrf, request)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, adminEmergencyFailure(err)
	}
	return result, nil
}

func (s *adminEmergencyCancellations) Acceptance(ctx context.Context, token string, strategyID, id int64) (domain.AdminEmergencyCancelAcceptance, error) {
	if strategyID <= 0 || id <= 0 {
		return domain.AdminEmergencyCancelAcceptance{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminEmergencyCancelAcceptance
	snapshots := adminStrategies{db: s.db}
	err := snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.AdminEmergencyCancellation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).
			Where("id = ? AND strategy_instance_id = ?", id, strategyID).Take(&row).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		result = adminEmergencyAcceptance(&row)
		return nil
	})
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	return result, nil
}

func requestAdminEmergencyCancellation(ctx context.Context, tx *gorm.DB, token, csrf string, request domain.AdminEmergencyCancelRequest) (domain.AdminEmergencyCancelAcceptance, error) {
	user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	fingerprint := adminEmergencyFingerprint(request)
	previous, found, err := previousAdminEmergency(ctx, tx, user.Id, request.Key, fingerprint)
	if err != nil || found {
		return previous, err
	}
	var instance modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", request.StrategyID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, adminRiskLookupFailure(err)
	}
	// Re-read after the strategy mutex. Same-key concurrent callers observe the
	// committed original before checking its now-historical expected revision.
	previous, found, err = previousAdminEmergency(ctx, tx, user.Id, request.Key, fingerprint)
	if err != nil || found {
		return previous, err
	}
	revision, err := adminRiskRevision(ctx, tx, instance.Id)
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	if revision != request.ExpectedRiskRevision {
		return domain.AdminEmergencyCancelAcceptance{}, domain.ErrAdminConflict
	}
	risk, err := changeAdminRisk(ctx, tx, token, csrf, domain.AdminRiskRequest{
		Key:              adminEmergencyRiskKey(request, fingerprint),
		StrategyID:       instance.Id,
		ExpectedRevision: revision,
		Action:           domain.AdminRiskBlock,
		Reason:           request.Reason,
	})
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	return recordAdminEmergency(ctx, tx, user, session, request, fingerprint, risk)
}

func previousAdminEmergency(ctx context.Context, tx *gorm.DB, actorID int64, key, fingerprint string) (domain.AdminEmergencyCancelAcceptance, bool, error) {
	var row modelv1.AdminEmergencyCancellation
	err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Where("actor_user_id = ? AND idempotency_key = ?", actorID, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.AdminEmergencyCancelAcceptance{}, false, nil
	}
	if err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, false, err
	}
	if row.RequestFingerprint != fingerprint {
		return domain.AdminEmergencyCancelAcceptance{}, false, domain.ErrAdminConflict
	}
	return adminEmergencyAcceptance(&row), true, nil
}

func recordAdminEmergency(ctx context.Context, tx *gorm.DB, user *modelv1.AdminUser, session *modelv1.AdminSession, request domain.AdminEmergencyCancelRequest, fingerprint string, risk domain.AdminRiskReceipt) (domain.AdminEmergencyCancelAcceptance, error) {
	audit := &modelv1.AdminAudit{
		Actor:         user.Username,
		ActorRole:     user.Role,
		Action:        domain.AdminEmergencyCancelAction,
		Resource:      domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(request.StrategyID, 10),
		Reason:        request.Reason,
		BeforeVersion: proto.Int64(request.ExpectedRiskRevision),
		AfterVersion:  proto.Int64(risk.Revision),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	row := &modelv1.AdminEmergencyCancellation{
		ActorUserId:          user.Id,
		ActorSessionId:       session.Id,
		StrategyInstanceId:   request.StrategyID,
		RiskChangeId:         risk.ID,
		AuditId:              audit.Id,
		IdempotencyKey:       request.Key,
		RequestFingerprint:   fingerprint,
		Scope:                string(request.Scope),
		ExpectedRiskRevision: request.ExpectedRiskRevision,
		RiskRevision:         risk.Revision,
		Reason:               request.Reason,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(row).Error; err != nil {
		return domain.AdminEmergencyCancelAcceptance{}, err
	}
	return adminEmergencyAcceptance(row), nil
}

func adminEmergencyAcceptance(row *modelv1.AdminEmergencyCancellation) domain.AdminEmergencyCancelAcceptance {
	return domain.AdminEmergencyCancelAcceptance{
		ID:                   row.Id,
		ActorUserID:          row.ActorUserId,
		StrategyID:           row.StrategyInstanceId,
		Scope:                domain.AdminEmergencyCancelScope(row.Scope),
		ExpectedRiskRevision: row.ExpectedRiskRevision,
		RiskRevision:         row.RiskRevision,
		Reason:               row.Reason,
		CreatedAt:            time.UnixMilli(row.CreateAt).UTC(),
	}
}

func adminEmergencyFingerprint(request domain.AdminEmergencyCancelRequest) string {
	data := binary.BigEndian.AppendUint64(nil, uint64(request.StrategyID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedRiskRevision))
	data = binary.BigEndian.AppendUint64(data, uint64(len(request.Scope)))
	data = append(data, request.Scope...)
	data = append(data, request.Reason...)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func adminEmergencyRiskKey(request domain.AdminEmergencyCancelRequest, fingerprint string) string {
	data := domain.AdminEmergencyCancelAction + "\x00" + request.Key + "\x00" + fingerprint
	digest := sha256.Sum256([]byte(data))
	return hex.EncodeToString(digest[:])
}

func adminEmergencyFailure(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminEmergencyCancel) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch {
		case databaseError.ConstraintName == "admin_emergency_authority":
			return domain.ErrUnauthenticated
		case databaseError.Code == "23505":
			return domain.ErrAdminConflict
		}
	}
	return adminRiskFailure(err)
}

func requireEmergencyScopeClear(ctx context.Context, tx *gorm.DB, strategyID int64) error {
	// Until completion proofs are implemented, every accepted scope is pending.
	// Keep this identical to the versioned final database recovery predicate.
	var pending int64
	if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).
		Where("strategy_instance_id = ? AND delete_at IS NULL", strategyID).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return domain.ErrAdminRiskRecoveryDenied
	}
	return nil
}

var _ domain.AdminEmergencyCancellations = (*adminEmergencyCancellations)(nil)
