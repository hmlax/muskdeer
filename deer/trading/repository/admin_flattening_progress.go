package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// Progress observes stored work; it never claims, repairs or completes it.
func (s *adminAccountFlattenings) Progress(ctx context.Context, token string, strategyID, flatteningID int64) (domain.AdminFlattenProgress, error) {
	if strategyID <= 0 || flatteningID <= 0 {
		return domain.AdminFlattenProgress{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminFlattenProgress
	snapshots := adminStrategies{db: s.db}
	err := snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row executionmodelv1.AdminAccountFlatteningTask
		if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).
			Where("flattening_id = ? AND strategy_instance_id = ? AND delete_at IS NULL", flatteningID, strategyID).Take(&row).Error; err != nil {
			return adminRiskLookupFailure(err)
		}
		var err error
		result, err = adminFlattenProgress(&row)
		return err
	})
	if err != nil {
		return domain.AdminFlattenProgress{}, err
	}
	return result, nil
}

func adminFlattenProgress(row *executionmodelv1.AdminAccountFlatteningTask) (domain.AdminFlattenProgress, error) {
	if row == nil || row.Id <= 0 || row.FlatteningId <= 0 || row.StrategyInstanceId <= 0 || row.InstrumentId <= 0 ||
		!coreIdentifier(row.AccountId, 64) || row.Revision <= 0 || row.CreateAt <= 0 || row.UpdateAt < row.CreateAt || row.DeleteAt != nil {
		return domain.AdminFlattenProgress{}, domain.ErrAdminUnavailable
	}
	state := domain.AdminFlattenTaskState(row.State)
	var claimed time.Time
	switch state {
	case domain.AdminFlattenQueued:
		if row.Revision != 1 || row.OwnerId != nil || row.OwnerEpoch != nil || row.ClaimedAt != nil {
			return domain.AdminFlattenProgress{}, domain.ErrAdminUnavailable
		}
	case domain.AdminFlattenActive:
		if row.Revision <= 1 || !ownerid.Valid(row.GetOwnerId()) || row.GetOwnerEpoch() <= 0 || row.GetClaimedAt() < row.CreateAt || row.GetClaimedAt() > row.UpdateAt {
			return domain.AdminFlattenProgress{}, domain.ErrAdminUnavailable
		}
		claimed = time.UnixMilli(row.GetClaimedAt()).UTC()
	default:
		return domain.AdminFlattenProgress{}, domain.ErrAdminUnavailable
	}
	return domain.AdminFlattenProgress{
		ID:           row.Id,
		FlatteningID: row.FlatteningId,
		StrategyID:   row.StrategyInstanceId,
		AccountID:    row.AccountId,
		InstrumentID: row.InstrumentId,
		State:        state,
		Revision:     row.Revision,
		OwnerID:      row.GetOwnerId(),
		OwnerEpoch:   row.GetOwnerEpoch(),
		ClaimedAt:    claimed,
		CreatedAt:    time.UnixMilli(row.CreateAt).UTC(),
		UpdatedAt:    time.UnixMilli(row.UpdateAt).UTC(),
	}, nil
}
