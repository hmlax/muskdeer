package instanceconfig

import (
	"context"

	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"gorm.io/gorm"
)

func guardLongGridEdit(ctx context.Context, db *gorm.DB, row *strategy.StrategyInstance) error {
	if row.Parameters.GetLongGrid() == nil {
		return nil
	}
	var count int64
	err := fresh(ctx, db).Model(&execution.LongGridRuntime{}).Where("instance_id = ? AND delete_at IS NULL", row.Id).
		Where("pending_action_id IS NOT NULL OR stop_action_id IS NOT NULL OR state = ? OR jsonb_array_length(coalesce(lots->'items','[]'::jsonb)) > 0", longgrid.Stopping).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	return nil
}
