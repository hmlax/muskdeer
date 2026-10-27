package repository

import (
	"context"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type riskCandidates struct{ db *gorm.DB }

// NewRiskCandidates borrows storage and performs no discovery or writes.
func NewRiskCandidates(db *gorm.DB) (domain.RiskCandidates, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &riskCandidates{db: db}, nil
}

func (r *riskCandidates) DesiredForAccount(ctx context.Context, accountID string) ([]int64, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if !coreIdentifier(accountID, 64) {
		return nil, domain.ErrInvalidOrder
	}
	var rows []*modelv1.StrategyInstance
	if err := r.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Select("Id").Where("account_id = ? AND desired_running = ? AND delete_at IS NULL", accountID, true).Order("id ASC").Limit(domain.MaxRunningRiskCandidates + 1).Find(&rows).Error; err != nil {
		return nil, coreFailure(ctx, err)
	}
	if len(rows) > domain.MaxRunningRiskCandidates {
		return nil, domain.ErrRiskDenied
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids, nil
}
