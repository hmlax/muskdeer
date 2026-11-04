package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

type adminFlattenWork struct{ db *gorm.DB }

// NewAdminFlattenWork borrows the process database without work or resources.
func NewAdminFlattenWork(db *gorm.DB) (domain.AdminFlattenWork, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminFlatten
	}
	return &adminFlattenWork{db: db}, nil
}

func (s *adminFlattenWork) Claim(ctx context.Context, token domain.LeaseToken, flatteningID int64) (domain.AdminFlattenProgress, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminFlattenProgress{}, err
	}
	if flatteningID <= 0 || token.Epoch <= 0 || token.Key.Kind != domain.LeaseKindStrategy {
		return domain.AdminFlattenProgress{}, domain.ErrInvalidAdminFlatten
	}
	if err := validateLeaseCall(ctx, token.Key, token.OwnerID, domain.MinLeaseTTL); err != nil {
		return domain.AdminFlattenProgress{}, err
	}
	var result domain.AdminFlattenProgress
	err := s.db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Transaction(func(tx *gorm.DB) error {
		instance, err := lockFlattenWorkOwner(ctx, tx, token)
		if err != nil {
			return err
		}
		var row executionmodelv1.AdminAccountFlatteningTask
		if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("flattening_id = ? AND strategy_instance_id = ? AND delete_at IS NULL", flatteningID, instance.Id).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		if _, err := adminFlattenProgress(&row); err != nil {
			return err
		}
		if err := revalidateFlattenWork(ctx, tx, instance, &row); err != nil {
			return err
		}
		if row.State == string(domain.AdminFlattenActive) && row.GetOwnerId() == token.OwnerID && row.GetOwnerEpoch() == token.Epoch {
			result, err = adminFlattenProgress(&row)
			return err // Stored claim observation, never an execution permission.
		}
		if row.GetOwnerEpoch() >= token.Epoch {
			return domain.ErrLeaseLost
		}
		var clock struct{ NowMS int64 }
		if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", row.Id).
			Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
			return err
		}
		previous := row.Revision
		row.State, row.Revision = string(domain.AdminFlattenActive), previous+1
		row.OwnerId, row.OwnerEpoch, row.ClaimedAt = proto.String(token.OwnerID), proto.Int64(token.Epoch), proto.Int64(clock.NowMS)
		row.UpdateAt = clock.NowMS
		updated := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ? AND revision = ?", row.Id, previous).
			Select("State", "Revision", "OwnerId", "OwnerEpoch", "ClaimedAt", "UpdateAt").UpdateColumns(&row)
		if err := reservationResult(updated); err != nil {
			return err
		}
		result, err = adminFlattenProgress(&row)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminFlattenProgress{}, flattenWorkFailure(ctx, err)
	}
	return result, nil
}

func lockFlattenWorkOwner(ctx context.Context, tx *gorm.DB, token domain.LeaseToken) (*strategymodelv1.StrategyInstance, error) {
	var row strategymodelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", token.Key.StrategyID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&row).Error; err != nil {
		return nil, err
	}
	lease, now, err := lockLease(ctx, tx, token.Key)
	if err != nil {
		return nil, err
	}
	if lease.GetOwnerId() != token.OwnerID || lease.GetEpoch() != token.Epoch || lease.GetExpiresAt() <= now ||
		row.RuntimeOwnerId != token.OwnerID || row.RuntimeOwnerEpoch != token.Epoch {
		return nil, domain.ErrLeaseLost
	}
	return &row, nil
}

func revalidateFlattenWork(ctx context.Context, tx *gorm.DB, instance *strategymodelv1.StrategyInstance, task *executionmodelv1.AdminAccountFlatteningTask) error {
	var confirmation executionmodelv1.AdminAccountFlattening
	if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Where("id = ? AND delete_at IS NULL", task.FlatteningId).Take(&confirmation).Error; err != nil {
		return err
	}
	receipt, err := adminFlattenReceipt(&confirmation)
	if err != nil {
		return err
	}
	if receipt.StrategyID != instance.Id || receipt.AccountID != instance.AccountId || receipt.InstrumentID != instance.InstrumentId ||
		task.AccountId != receipt.AccountID || task.InstrumentId != receipt.InstrumentID {
		return domain.ErrAdminFlattenDenied
	}
	core, err := lockCoreRows(ctx, tx, receipt.AccountID, receipt.InstrumentID)
	if err != nil {
		return err
	}
	var account commonmodelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&commonmodelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", receipt.AccountID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Take(&account).Error; err != nil {
		return err
	}
	basis, err := adminFlattenBasis(ctx, tx, instance, core, &account)
	if err != nil {
		return err
	}
	basis.Cancellation, err = flattenCancellationProof(ctx, tx, receipt.StrategyID, receipt.CancellationID)
	if err != nil {
		return err
	}
	_, err = domain.PlanAdminFlatten(domain.AdminFlattenRequest{
		Key:                       confirmation.IdempotencyKey,
		Scope:                     receipt.Scope,
		StrategyID:                receipt.StrategyID,
		AccountID:                 receipt.AccountID,
		InstrumentID:              receipt.InstrumentID,
		CancellationID:            receipt.CancellationID,
		ExpectedRiskRevision:      receipt.ExpectedRiskRevision,
		ExpectedPortfolioRevision: receipt.ExpectedPortfolioRevision,
		ExpectedBaseRevision:      receipt.ExpectedBaseRevision,
		ConfirmedQuantity:         receipt.ConfirmedQuantity,
		LimitPrice:                receipt.LimitPrice,
		Reason:                    receipt.Reason,
	}, basis)
	return err
}

func flattenWorkFailure(ctx context.Context, err error) error {
	if errors.Is(err, domain.ErrAdminFlattenDenied) || errors.Is(err, domain.ErrAdminConflict) || errors.Is(err, domain.ErrAdminUnavailable) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.ConstraintName {
		case "admin_flatten_task_owner":
			return domain.ErrLeaseLost
		case "uk_admin_flatten_task_active_account":
			return domain.ErrAdminConflict
		case "admin_flatten_task_clock", "admin_flatten_task_history", "ck_admin_flatten_task_claim":
			return domain.ErrAdminFlattenDenied
		}
	}
	return coreFailure(ctx, err)
}

var _ domain.AdminFlattenWork = (*adminFlattenWork)(nil)
