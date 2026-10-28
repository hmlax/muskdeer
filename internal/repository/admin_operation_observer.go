package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// NewAdminOperationObserver only borrows observation/result storage. It grants
// no user authority and cannot change runtime, desired state, risk or funds.
func NewAdminOperationObserver(db *gorm.DB) (domain.AdminOperationObserver, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminControl
	}
	return &adminStrategyControl{db: db}, nil
}

func (s *adminStrategyControl) Pending(ctx context.Context, after int64, limit int) ([]int64, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if !validAdminPage(domain.AdminPage{
		AfterID: after,
		Limit:   limit,
	}) {
		return nil, domain.ErrInvalidAdminControl
	}
	var rows []*modelv1.AdminStrategyOperation
	err := s.db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Select("Id").
		Where("id > ? AND state = ? AND delete_at IS NULL", after, domain.AdminOperationAccepted).Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, adminControlFailure(err)
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids, nil
}

func (s *adminStrategyControl) Observe(ctx context.Context, id int64) (bool, error) {
	if err := coreContext(ctx); err != nil {
		return false, err
	}
	if id <= 0 {
		return false, domain.ErrInvalidAdminControl
	}
	completed := false
	err := s.db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Transaction(func(tx *gorm.DB) error {
		var initial modelv1.AdminStrategyOperation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).Take(&initial).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		if initial.State != string(domain.AdminOperationAccepted) {
			return nil
		}
		// Strategy before operation: management, runtime and observers share
		// the same lock order. Result projection never claims a controller.
		var strategy modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", initial.StrategyInstanceId).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&strategy).Error; err != nil {
			return err
		}
		var operation modelv1.AdminStrategyOperation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&operation).Error; err != nil {
			return err
		}
		if operation.State != string(domain.AdminOperationAccepted) {
			return nil
		}
		state, outcome := domain.AdminOperationSucceeded, domain.AdminOperationOutcomeReached
		if strategy.DeleteAt != nil || strategy.DesiredState != operation.TargetState || strategy.RequestedConfigVersion != operation.ConfigVersion || strategy.MarketDataSource != operation.MarketDataSource {
			state, outcome = domain.AdminOperationSuperseded, domain.AdminOperationOutcomeReplaced
		} else {
			ready, err := adminOperationReady(ctx, tx, &strategy, &operation)
			if err != nil || !ready {
				return err
			}
		}
		now, err := adminOperationTime(ctx, tx, id)
		if err != nil {
			return err
		}
		updated := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ? AND state = ? AND revision = ?", id, domain.AdminOperationAccepted, operation.Revision).
			Select("State", "OutcomeCode", "Revision", "CompletedAt", "ObservedRevision", "ObservedState", "ObservedOwnerId", "ObservedOwnerEpoch").
			Updates(&modelv1.AdminStrategyOperation{
				State:              string(state),
				OutcomeCode:        outcome,
				Revision:           operation.Revision + 1,
				CompletedAt:        proto.Int64(now),
				ObservedRevision:   proto.Int64(strategy.Revision),
				ObservedState:      proto.String(strategy.State),
				ObservedOwnerId:    proto.String(strategy.RuntimeOwnerId),
				ObservedOwnerEpoch: proto.Int64(strategy.RuntimeOwnerEpoch),
			})
		if err := reservationResult(updated); err != nil {
			return err
		}
		completed = true
		return nil
	})
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.ConstraintName == "admin_operation_lease_not_ready" {
		// The observed lease can expire before the final database check.
		// The result transaction rolled back; retain ACCEPTED for observation.
		return false, nil
	}
	if err != nil {
		return false, adminControlFailure(err)
	}
	return completed, nil
}

func adminOperationReady(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, operation *modelv1.AdminStrategyOperation) (bool, error) {
	if strategy.State != operation.TargetState {
		return false, nil
	}
	if operation.TargetState != string(domain.StrategyStateRunning) {
		err := requireStrategyClear(ctx, tx, strategy.Id)
		if errors.Is(err, domain.ErrOutcomeUnknown) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var reserved int64
		err = tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).
			Where("strategy_instance_id = ? AND delete_at IS NULL AND reserved_notional <> ?", strategy.Id, 0).Count(&reserved).Error
		return err == nil && reserved == 0, err
	}
	if strategy.RuntimeOwnerEpoch <= 0 || strategy.GetActiveConfigVersion() != operation.ConfigVersion || strategy.GetActiveMarketDataSource() != operation.MarketDataSource {
		return false, nil
	}
	var lease modelv1.StrategyLease
	err := tx.WithContext(ctx).Model(&modelv1.StrategyLease{}).Where("strategy_instance_id = ? AND delete_at IS NULL", strategy.Id).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&lease).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now, err := adminOperationTime(ctx, tx, operation.Id)
	return err == nil && lease.GetOwnerId() == strategy.RuntimeOwnerId && lease.GetEpoch() == strategy.RuntimeOwnerEpoch && lease.GetExpiresAt() > now, err
}

func adminOperationTime(ctx context.Context, tx *gorm.DB, id int64) (int64, error) {
	var clock struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
	return clock.NowMS, err
}

var _ domain.AdminOperationObserver = (*adminStrategyControl)(nil)
