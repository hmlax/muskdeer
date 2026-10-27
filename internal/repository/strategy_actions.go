package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type strategyActions struct{ db *gorm.DB }

// NewStrategyActions borrows the root Deer database. It does not migrate,
// initialize strategy state, grant authority, or send an execution request.
func NewStrategyActions(db *gorm.DB) (domain.StrategyActions, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyActions{db: db}, nil
}

func (s *strategyActions) Plan(ctx context.Context, authority domain.StrategyAuthority, plan domain.StrategyActionPlan) (domain.StrategyAction, error) {
	if err := validateActionPlan(ctx, plan); err != nil {
		return domain.StrategyAction{}, err
	}
	if err := actionAuthorityFields(authority); err != nil {
		return domain.StrategyAction{}, err
	}
	var result domain.StrategyAction
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Transaction(func(tx *gorm.DB) error {
		strategy, _, err := lockActionAuthority(ctx, tx, authority)
		if err != nil {
			return err
		}
		if strategy.AccountId != plan.AccountID || strategy.InstrumentId != plan.InstrumentID {
			return domain.ErrInvalidOrder
		}
		var existing modelv1.StrategyAction
		err = tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).
			Where("strategy_instance_id = ? AND decision_key = ?", authority.StrategyID, plan.Key).First(&existing).Error
		if err == nil {
			old, err := actionSnapshot(&existing)
			if err != nil {
				return err
			}
			if !sameActionPlan(old, plan, authority.ConfigVersion) {
				return domain.ErrIdempotencyConflict
			}
			result = old
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := actionPlanCapacity(ctx, tx, strategy, plan); err != nil {
			return err
		}
		row, err := newActionRow(authority, plan)
		if err != nil {
			return err
		}
		if err := validateActionTarget(ctx, tx, row); err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Create(row).Error; err != nil {
			return err
		}
		result, err = actionSnapshot(row)
		return err
	})
	if err != nil {
		return domain.StrategyAction{}, actionFailure(ctx, err)
	}
	return result, nil
}

func (s *strategyActions) ByKey(ctx context.Context, strategyID int64, key string) (domain.StrategyAction, error) {
	if err := coreContext(ctx); err != nil {
		return domain.StrategyAction{}, err
	}
	if strategyID <= 0 || !coreIdentifier(key, 128) {
		return domain.StrategyAction{}, domain.ErrInvalidOrder
	}
	var row modelv1.StrategyAction
	if err := s.db.WithContext(ctx).Model(&modelv1.StrategyAction{}).
		Where("strategy_instance_id = ? AND decision_key = ? AND delete_at IS NULL", strategyID, key).First(&row).Error; err != nil {
		return domain.StrategyAction{}, actionFailure(ctx, err)
	}
	return actionSnapshot(&row)
}

func (s *strategyActions) Active(ctx context.Context, strategyID int64) ([]domain.StrategyAction, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if strategyID <= 0 {
		return nil, domain.ErrInvalidOrder
	}
	var rows []*modelv1.StrategyAction
	if err := s.db.WithContext(ctx).Model(&modelv1.StrategyAction{}).
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategyID, activeActionStates()).
		Order("id ASC").Limit(domain.MaxActiveStrategyActions + 1).Find(&rows).Error; err != nil {
		return nil, actionFailure(ctx, err)
	}
	if len(rows) > domain.MaxActiveStrategyActions {
		return nil, domain.ErrRiskDenied
	}
	result := make([]domain.StrategyAction, 0, len(rows))
	for _, row := range rows {
		action, err := actionSnapshot(row)
		if err != nil {
			return nil, err
		}
		result = append(result, action)
	}
	return result, nil
}

func actionPlanCapacity(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, plan domain.StrategyActionPlan) error {
	var active int64
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).
		Where("strategy_instance_id = ? AND state IN ? AND delete_at IS NULL", strategy.Id, activeActionStates()).Count(&active).Error; err != nil {
		return err
	}
	if active >= domain.MaxActiveStrategyActions || plan.Kind == domain.StrategyActionSubmit && active != 0 {
		return domain.ErrOutcomeUnknown
	}
	if plan.Kind == domain.StrategyActionSubmit {
		if strategy.State != string(domain.StrategyStateRunning) || !strategy.DesiredRunning {
			return domain.ErrRiskDenied
		}
		return nil
	}
	var cancellations int64
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).
		Where("strategy_instance_id = ? AND kind = ? AND intent_id = ? AND state IN ? AND delete_at IS NULL",
			strategy.Id, domain.StrategyActionCancel, plan.TargetIntentID, activeActionStates()).Count(&cancellations).Error; err != nil {
		return err
	}
	if cancellations != 0 {
		return domain.ErrOutcomeUnknown
	}
	return nil
}

func validateActionTarget(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyAction) error {
	if row.Kind != string(domain.StrategyActionCancel) {
		return nil
	}
	var parent modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
		Where("id = ? AND delete_at IS NULL", row.GetIntentId()).Clauses(clause.Locking{Strength: "SHARE"}).First(&parent).Error; err != nil {
		return err
	}
	if parent.AccountId != row.AccountId || parent.StrategyInstanceId != row.StrategyInstanceId || parent.InstrumentId != row.InstrumentId || parent.ClientOrderId != row.ClientOrderId {
		return domain.ErrInvalidOrder
	}
	return nil
}
