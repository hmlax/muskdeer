package repository

import (
	"context"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type strategyCandidates struct{ db *gorm.DB }

// NewStrategyCandidates borrows storage without creating or claiming instances.
func NewStrategyCandidates(db *gorm.DB) (domain.StrategyCandidates, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyCandidates{db: db}, nil
}

func (s *strategyCandidates) ForAccount(ctx context.Context, accountID string) ([]int64, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if !coreIdentifier(accountID, 64) {
		return nil, domain.ErrInvalidOrder
	}
	var rows []*modelv1.StrategyInstance
	if err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Select("Id").
		Where("account_id = ? AND delete_at IS NULL", accountID).Order("id ASC").Limit(domain.MaxAccountStrategyInstances + 1).Find(&rows).Error; err != nil {
		return nil, coreFailure(ctx, err)
	}
	if len(rows) > domain.MaxAccountStrategyInstances {
		return nil, domain.ErrRiskDenied
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids, nil
}
