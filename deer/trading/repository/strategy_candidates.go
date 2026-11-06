package repository

import (
	"context"

	"gorm.io/gorm"

	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
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
	var rows []*strategymodelv1.StrategyInstance
	if err := s.db.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Select("Id").
		Where("account_id = ? AND delete_at IS NULL", accountID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, coreFailure(ctx, err)
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids, nil
}
