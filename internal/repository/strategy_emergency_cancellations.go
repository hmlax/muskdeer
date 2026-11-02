package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type strategyEmergencyCancellations struct{ db *gorm.DB }

// NewStrategyEmergencyCancellations borrows storage for bounded metadata work.
// It opens no resource and never sends an execution command.
func NewStrategyEmergencyCancellations(db *gorm.DB) (domain.StrategyEmergencyCancellations, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	return &strategyEmergencyCancellations{db: db}, nil
}

func (s *strategyEmergencyCancellations) Prepare(ctx context.Context, strategyID, afterID int64) (domain.AdminEmergencyWork, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminEmergencyWork{}, err
	}
	if strategyID <= 0 || afterID < 0 {
		return domain.AdminEmergencyWork{}, domain.ErrInvalidAdminEmergencyCancel
	}
	var result domain.AdminEmergencyWork
	err := s.db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Transaction(func(tx *gorm.DB) error {
		var instance modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", strategyID).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
			return err
		}
		var accepted modelv1.AdminEmergencyCancellation
		err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).
			Where("strategy_instance_id = ? AND id > ? AND delete_at IS NULL", strategyID, afterID).
			Where("id NOT IN (?)", completedEmergencyScopes(ctx, tx, strategyID)).Order("id ASC").Take(&accepted).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var task modelv1.AdminEmergencyCancellationTask
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.Id).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&task).Error; err != nil {
			return err
		}
		result, err = advanceEmergencyScope(ctx, tx, &instance, &accepted, &task)
		return err
	})
	if err != nil {
		return domain.AdminEmergencyWork{}, coreFailure(ctx, err)
	}
	return result, nil
}

func advanceEmergencyScope(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, accepted *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) (domain.AdminEmergencyWork, error) {
	var budget modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND delete_at IS NULL", instance.Id).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&budget).Error; err != nil {
		return domain.AdminEmergencyWork{}, err
	}
	if budget.Enabled {
		return domain.AdminEmergencyWork{}, domain.ErrRiskDenied
	}
	before := proto.CloneOf(task)
	progress, err := emergencyProgress(ctx, tx, accepted.Id, instance.Id, task.EnrollmentComplete)
	if err != nil {
		return domain.AdminEmergencyWork{}, err
	}
	if progress.PendingSubmissions == 0 {
		if err := enrollEmergencyTargets(ctx, tx, instance, accepted, task); err != nil {
			return domain.AdminEmergencyWork{}, err
		}
		progress, err = emergencyProgress(ctx, tx, accepted.Id, instance.Id, task.EnrollmentComplete)
		if err != nil {
			return domain.AdminEmergencyWork{}, err
		}
		state, err := progress.State()
		if err != nil {
			return domain.AdminEmergencyWork{}, err
		}
		if state != domain.AdminEmergencyCancelAccepted {
			projectionClear, err := emergencyCoreProjectionClear(ctx, tx, instance.Id, accepted.Id, state)
			if err != nil {
				return domain.AdminEmergencyWork{}, err
			}
			if projectionClear {
				task.State = string(state)
				now, err := emergencyDatabaseTime(ctx, tx, instance.Id)
				if err != nil {
					return domain.AdminEmergencyWork{}, err
				}
				task.CompletedAt = proto.Int64(now)
			}
		}
	}
	if !proto.Equal(before, task) {
		if err := saveEmergencyTask(ctx, tx, task); err != nil {
			return domain.AdminEmergencyWork{}, err
		}
	}
	result := domain.AdminEmergencyWork{
		AcceptanceID: accepted.Id,
		TaskID:       task.Id,
		State:        domain.AdminEmergencyCancelState(task.State),
		Progress:     progress,
		Revision:     task.Revision,
	}
	if task.CompletedAt != nil {
		result.CompletedAt = time.UnixMilli(*task.CompletedAt).UTC()
	}
	return result, nil
}

func enrollEmergencyTargets(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, accepted *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) error {
	if task.EnrollmentComplete {
		return nil
	}
	if task.EnrollmentUpperId == nil {
		var maximum struct{ ID int64 }
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ? AND delete_at IS NULL", instance.Id).
			Select("COALESCE(max(id), 0)::bigint AS id").Take(&maximum).Error; err != nil {
			return err
		}
		task.EnrollmentUpperId = proto.Int64(maximum.ID)
	}
	if task.GetEnrollmentUpperId() == 0 {
		task.EnrollmentComplete = true
		return nil
	}
	var pending int64
	if err := tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).
		Where("strategy_instance_id = ? AND state = ?", instance.Id, domain.AdminCancellationAccepted).Count(&pending).Error; err != nil {
		return err
	}
	if pending >= domain.MaxActiveStrategyActions {
		return nil
	}
	limit := min(domain.MaxEmergencyTargetsPerStep, domain.MaxActiveStrategyActions-int(pending))
	var targets []*modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("strategy_instance_id = ? AND account_id = ? AND instrument_id = ? AND delete_at IS NULL AND id > ? AND id <= ?", instance.Id, instance.AccountId, instance.InstrumentId, task.EnrollmentAfterId, task.GetEnrollmentUpperId()).
		Where("state IN ? OR reserved_quote_notional <> 0", emergencyActiveOrderStates()).Order("id ASC").Limit(limit).
		Clauses(clause.Locking{Strength: "SHARE"}).Find(&targets).Error; err != nil {
		return err
	}
	var audit modelv1.AdminAudit
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", accepted.AuditId).Take(&audit).Error; err != nil {
		return err
	}
	for _, order := range targets {
		if err := recordEmergencyTarget(ctx, tx, accepted, &audit, order); err != nil {
			return err
		}
		task.EnrollmentAfterId = order.Id
	}
	if len(targets) < limit {
		task.EnrollmentAfterId = task.GetEnrollmentUpperId()
		task.EnrollmentComplete = true
	}
	return nil
}

func recordEmergencyTarget(ctx context.Context, tx *gorm.DB, accepted *modelv1.AdminEmergencyCancellation, originalAudit *modelv1.AdminAudit, order *modelv1.OrderIntent) error {
	keyData := binary.BigEndian.AppendUint64([]byte(domain.AdminEmergencyCancelAction), uint64(accepted.Id))
	keyData = binary.BigEndian.AppendUint64(keyData, uint64(order.Id))
	digest := sha256.Sum256(keyData)
	request, err := (domain.AdminCancellationRequest{
		Key:                   hex.EncodeToString(digest[:]),
		StrategyID:            accepted.StrategyInstanceId,
		OrderID:               order.Id,
		ExpectedOrderRevision: order.Revision,
		Reason:                accepted.Reason,
	}).Normalize()
	if err != nil {
		return err
	}
	now, err := emergencyDatabaseTime(ctx, tx, accepted.StrategyInstanceId)
	if err != nil {
		return err
	}
	audit := &modelv1.AdminAudit{
		CreateAt:      now,
		UpdateAt:      now,
		Actor:         originalAudit.Actor,
		ActorRole:     originalAudit.ActorRole,
		Action:        domain.AdminCancellationAction,
		Resource:      domain.AdminAuditResourceOrder + ":" + strconv.FormatInt(order.Id, 10),
		Reason:        accepted.Reason,
		BeforeVersion: proto.Int64(order.Revision),
		AfterVersion:  proto.Int64(order.Revision),
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Create(&modelv1.AdminOrderCancellation{
		CreateAt:                now,
		UpdateAt:                now,
		ActorUserId:             accepted.ActorUserId,
		ActorSessionId:          accepted.ActorSessionId,
		StrategyInstanceId:      accepted.StrategyInstanceId,
		OrderIntentId:           order.Id,
		AuditId:                 audit.Id,
		IdempotencyKey:          request.Key,
		RequestFingerprint:      adminCancellationFingerprint(request),
		ExpectedOrderRevision:   order.Revision,
		Reason:                  accepted.Reason,
		State:                   string(domain.AdminCancellationAccepted),
		OutcomeCode:             domain.AdminCancellationPending,
		Revision:                1,
		EmergencyCancellationId: proto.Int64(accepted.Id),
	}).Error
}

func saveEmergencyTask(ctx context.Context, tx *gorm.DB, task *modelv1.AdminEmergencyCancellationTask) error {
	previous := task.Revision
	task.Revision++
	return reservationResult(tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("id = ? AND revision = ?", task.Id, previous).
		Select("EnrollmentUpperId", "EnrollmentAfterId", "EnrollmentComplete", "State", "Revision", "CompletedAt").Updates(task))
}

func emergencyDatabaseTime(ctx context.Context, tx *gorm.DB, strategyID int64) (int64, error) {
	var observed struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&observed).Error
	return observed.NowMS, err
}

func completedEmergencyScopes(ctx context.Context, tx *gorm.DB, strategyID int64) *gorm.DB {
	return tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Select("emergency_cancellation_id").
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategyID, []domain.AdminEmergencyCancelState{domain.AdminEmergencyCancelSucceeded, domain.AdminEmergencyCancelFailed})
}

var _ domain.StrategyEmergencyCancellations = (*strategyEmergencyCancellations)(nil)
