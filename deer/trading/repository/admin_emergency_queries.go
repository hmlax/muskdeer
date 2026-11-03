package repository

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func (s *adminEmergencyCancellations) Observation(ctx context.Context, token string, strategyID, id int64) (domain.AdminEmergencyCancelObservation, error) {
	if strategyID <= 0 || id <= 0 {
		return domain.AdminEmergencyCancelObservation{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminEmergencyCancelObservation
	err := s.emergencySnapshot(ctx, token, strategyID, func(tx *gorm.DB, now time.Time) error {
		var accepted modelv1.AdminEmergencyCancellation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).
			Where("id = ? AND strategy_instance_id = ? AND delete_at IS NULL", id, strategyID).Take(&accepted).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		var task modelv1.AdminEmergencyCancellationTask
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).
			Where("emergency_cancellation_id = ? AND strategy_instance_id = ? AND delete_at IS NULL", id, strategyID).Take(&task).Error; err != nil {
			return emergencyTaskLookupFailure(err)
		}
		record, err := adminEmergencyRecord(&accepted, &task)
		if err != nil {
			return err
		}
		progress, err := emergencyProgress(ctx, tx, id, strategyID, task.EnrollmentComplete)
		if err != nil {
			return err
		}
		if _, err := progress.State(); err != nil {
			return domain.ErrAdminUnavailable
		}
		result = domain.AdminEmergencyCancelObservation{
			Record:     record,
			Progress:   progress,
			ObservedAt: now,
		}
		return nil
	})
	if err != nil {
		return domain.AdminEmergencyCancelObservation{}, err
	}
	return result, nil
}

func (s *adminEmergencyCancellations) History(ctx context.Context, token string, strategyID int64, page domain.AdminPage) (domain.AdminEmergencyCancelPage, error) {
	if strategyID <= 0 || !validAdminPage(page) {
		return domain.AdminEmergencyCancelPage{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminEmergencyCancelPage
	err := s.emergencySnapshot(ctx, token, strategyID, func(tx *gorm.DB, now time.Time) error {
		var rows []*modelv1.AdminEmergencyCancellation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).
			Where("strategy_instance_id = ? AND id > ? AND delete_at IS NULL", strategyID, page.AfterID).
			Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		hasMore := len(rows) > page.Limit
		if hasMore {
			rows = rows[:page.Limit]
		}
		items, err := adminEmergencyRecords(ctx, tx, strategyID, rows)
		if err != nil {
			return err
		}
		result = domain.AdminEmergencyCancelPage{
			Items:      items,
			ObservedAt: now,
		}
		if hasMore {
			result.NextAfterID = items[len(items)-1].Acceptance.ID
		}
		return nil
	})
	if err != nil {
		return domain.AdminEmergencyCancelPage{}, err
	}
	return result, nil
}

func (s *adminEmergencyCancellations) emergencySnapshot(ctx context.Context, token string, strategyID int64, read func(*gorm.DB, time.Time) error) error {
	snapshots := adminStrategies{db: s.db}
	return snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		now, err := emergencyDatabaseTime(ctx, tx, strategyID)
		if err != nil {
			return adminRiskLookupFailure(err)
		}
		if now <= 0 {
			return domain.ErrAdminUnavailable
		}
		return read(tx, time.UnixMilli(now).UTC())
	})
}

func adminEmergencyRecords(ctx context.Context, tx *gorm.DB, strategyID int64, rows []*modelv1.AdminEmergencyCancellation) ([]domain.AdminEmergencyCancelRecord, error) {
	items := make([]domain.AdminEmergencyCancelRecord, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	var tasks []*modelv1.AdminEmergencyCancellationTask
	if err := tx.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).
		Where("emergency_cancellation_id IN ? AND strategy_instance_id = ? AND delete_at IS NULL", ids, strategyID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	byAcceptance := make(map[int64]*modelv1.AdminEmergencyCancellationTask, len(tasks))
	for _, task := range tasks {
		byAcceptance[task.EmergencyCancellationId] = task
	}
	for _, row := range rows {
		record, err := adminEmergencyRecord(row, byAcceptance[row.Id])
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, nil
}

func adminEmergencyRecord(row *modelv1.AdminEmergencyCancellation, task *modelv1.AdminEmergencyCancellationTask) (domain.AdminEmergencyCancelRecord, error) {
	if row == nil || task == nil || row.Id <= 0 || row.ActorUserId <= 0 || row.StrategyInstanceId <= 0 || row.CreateAt <= 0 || row.DeleteAt != nil ||
		row.Scope != string(domain.AdminEmergencyCancelScopeStrategy) || row.ExpectedRiskRevision <= 0 || row.ExpectedRiskRevision == math.MaxInt64 || row.RiskRevision != row.ExpectedRiskRevision+1 ||
		task.Id <= 0 || task.EmergencyCancellationId != row.Id || task.StrategyInstanceId != row.StrategyInstanceId || task.Revision <= 0 || task.DeleteAt != nil {
		return domain.AdminEmergencyCancelRecord{}, domain.ErrAdminUnavailable
	}
	state := domain.AdminEmergencyCancelState(task.State)
	switch state {
	case domain.AdminEmergencyCancelAccepted:
		if task.CompletedAt != nil {
			return domain.AdminEmergencyCancelRecord{}, domain.ErrAdminUnavailable
		}
	case domain.AdminEmergencyCancelSucceeded, domain.AdminEmergencyCancelFailed:
		if !task.EnrollmentComplete || task.CompletedAt == nil || *task.CompletedAt < row.CreateAt {
			return domain.AdminEmergencyCancelRecord{}, domain.ErrAdminUnavailable
		}
	default:
		return domain.AdminEmergencyCancelRecord{}, domain.ErrAdminUnavailable
	}
	result := domain.AdminEmergencyCancelRecord{
		Acceptance:         adminEmergencyAcceptance(row),
		State:              state,
		Revision:           task.Revision,
		EnrollmentComplete: task.EnrollmentComplete,
	}
	if task.CompletedAt != nil {
		result.CompletedAt = time.UnixMilli(*task.CompletedAt).UTC()
	}
	return result, nil
}

func emergencyTaskLookupFailure(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrAdminUnavailable
	}
	return err
}
