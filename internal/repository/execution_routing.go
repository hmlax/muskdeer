package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type executionRouting struct{ db *gorm.DB }

// NewExecutionRouting borrows storage without acquiring a shard or caching it.
func NewExecutionRouting(db *gorm.DB) (domain.ExecutionRouting, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &executionRouting{db: db}, nil
}

func (r *executionRouting) ExecutionInstance(ctx context.Context, accountID string) (string, error) {
	if err := coreContext(ctx); err != nil {
		return "", err
	}
	if !coreIdentifier(accountID, 64) {
		return "", domain.ErrInvalidOrder
	}
	var row modelv1.ExecutionShardLease
	err := r.db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ? AND delete_at IS NULL AND epoch > 0", accountID).
		Where("expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", domain.ErrLeaseLost
	}
	if err != nil {
		return "", coreFailure(ctx, err)
	}
	instance, valid := ownerid.Instance(row.OwnerId)
	if !valid {
		return "", domain.ErrLeaseLost
	}
	return instance, nil
}
