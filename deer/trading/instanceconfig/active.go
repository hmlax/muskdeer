package instanceconfig

import (
	"context"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
)

// Active checks the current control and configuration before using a startup
// snapshot. Legacy, non-instance fixtures retain their existing admission path.
func Active(ctx context.Context, db *gorm.DB, id, revision int64) (bool, error) {
	if id == 0 {
		return true, nil
	}
	var count int64
	err := fresh(ctx, db).Model(&model.StrategyInstance{}).
		Where("id = ? AND revision = ? AND desired_running = ? AND delete_at IS NULL", id, revision, true).Count(&count).Error
	return count == 1, err
}
