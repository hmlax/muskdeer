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

type adminOrderCancellations struct{ db *gorm.DB }

// NewAdminOrderCancellations borrows storage; requests do not send commands.
func NewAdminOrderCancellations(db *gorm.DB) (domain.AdminOrderCancellations, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminCancellation
	}
	return &adminOrderCancellations{db: db}, nil
}

func (s *adminOrderCancellations) Request(ctx context.Context, token, csrf string, request domain.AdminCancellationRequest) (domain.AdminCancellationReceipt, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	var result domain.AdminCancellationReceipt
	err = s.db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = requestAdminCancellation(ctx, tx, token, csrf, request)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminCancellationReceipt{}, adminCancellationFailure(err)
	}
	return result, nil
}

func (s *adminOrderCancellations) Receipt(ctx context.Context, token string, strategyID, id int64) (domain.AdminCancellationReceipt, error) {
	if strategyID <= 0 || id <= 0 {
		return domain.AdminCancellationReceipt{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminCancellationReceipt
	snapshots := adminStrategies{db: s.db}
	err := snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.AdminOrderCancellation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
			Where("id = ? AND strategy_instance_id = ?", id, strategyID).Take(&row).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		result = adminCancellationReceipt(&row)
		return nil
	})
	if err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	return result, nil
}

func requestAdminCancellation(ctx context.Context, tx *gorm.DB, token, csrf string, request domain.AdminCancellationRequest) (domain.AdminCancellationReceipt, error) {
	user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
	if err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	fingerprint := adminCancellationFingerprint(request)
	var previous modelv1.AdminOrderCancellation
	err = tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
		Where("actor_user_id = ? AND idempotency_key = ?", user.Id, request.Key).Take(&previous).Error
	if err == nil {
		if previous.RequestFingerprint != fingerprint {
			return domain.AdminCancellationReceipt{}, domain.ErrAdminConflict
		}
		return adminCancellationReceipt(&previous), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.AdminCancellationReceipt{}, err
	}
	var instance modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", request.StrategyID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
		return domain.AdminCancellationReceipt{}, adminRiskLookupFailure(err)
	}
	var order modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("id = ? AND strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL", request.OrderID, instance.Id, instance.AccountId, instance.InstrumentId).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&order).Error; err != nil {
		return domain.AdminCancellationReceipt{}, adminRiskLookupFailure(err)
	}
	if order.Revision != request.ExpectedOrderRevision {
		return domain.AdminCancellationReceipt{}, domain.ErrAdminConflict
	}
	var pending int64
	if err := tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
		Where("strategy_instance_id = ? AND state = ?", instance.Id, domain.AdminCancellationAccepted).Count(&pending).Error; err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	if pending >= domain.MaxActiveStrategyActions {
		return domain.AdminCancellationReceipt{}, domain.ErrAdminConflict
	}
	audit := &modelv1.AdminAudit{
		Actor:         user.Username,
		ActorRole:     user.Role,
		Action:        domain.AdminCancellationAction,
		Resource:      domain.AdminAuditResourceOrder + ":" + strconv.FormatInt(order.Id, 10),
		Reason:        request.Reason,
		BeforeVersion: proto.Int64(order.Revision),
		AfterVersion:  proto.Int64(order.Revision),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	row := &modelv1.AdminOrderCancellation{
		ActorUserId:           user.Id,
		ActorSessionId:        session.Id,
		StrategyInstanceId:    instance.Id,
		OrderIntentId:         order.Id,
		AuditId:               audit.Id,
		IdempotencyKey:        request.Key,
		RequestFingerprint:    fingerprint,
		ExpectedOrderRevision: order.Revision,
		Reason:                request.Reason,
		State:                 string(domain.AdminCancellationAccepted),
		OutcomeCode:           domain.AdminCancellationPending,
		Revision:              1,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Create(row).Error; err != nil {
		return domain.AdminCancellationReceipt{}, err
	}
	return adminCancellationReceipt(row), nil
}

func adminCancellationFingerprint(request domain.AdminCancellationRequest) string {
	data := binary.BigEndian.AppendUint64(nil, uint64(request.StrategyID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.OrderID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedOrderRevision))
	data = append(data, request.Reason...)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func adminCancellationReceipt(row *modelv1.AdminOrderCancellation) domain.AdminCancellationReceipt {
	result := domain.AdminCancellationReceipt{
		ID:                    row.Id,
		ActorUserID:           row.ActorUserId,
		StrategyID:            row.StrategyInstanceId,
		OrderID:               row.OrderIntentId,
		ExpectedOrderRevision: row.ExpectedOrderRevision,
		Reason:                row.Reason,
		State:                 domain.AdminCancellationState(row.State),
		Outcome:               row.OutcomeCode,
		Revision:              row.Revision,
		CreatedAt:             time.UnixMilli(row.CreateAt).UTC(),
		ObservedOrderRevision: row.GetObservedOrderRevision(),
		ObservedOrderState:    domain.OrderState(row.GetObservedOrderState()),
	}
	if row.CompletedAt != nil {
		result.CompletedAt = time.UnixMilli(*row.CompletedAt).UTC()
	}
	return result
}

func adminCancellationFailure(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminCancellation) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.ConstraintName == "admin_cancellation_authority" {
		return domain.ErrUnauthenticated
	}
	return adminControlFailure(err)
}

var _ domain.AdminOrderCancellations = (*adminOrderCancellations)(nil)
