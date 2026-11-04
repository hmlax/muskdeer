package venue

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
)

// abandonUnsent is legal only in the invocation that successfully committed the
// claim and positively knows that it never entered the SDK. A lost SDK reply or
// process crash keeps DISPATCHED. Another invocation cannot reuse the claim.
func (s *Store) abandonUnsent(ctx context.Context, admission Admission) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.db.WithContext(cleanup).Model(&executionmodelv1.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		account := AccountID(admission.UserID, admission.FundID)
		var lease commonmodelv1.ExecutionShardLease
		if err := tx.WithContext(cleanup).Model(&commonmodelv1.ExecutionShardLease{}).
			Where("account_id = ? AND delete_at IS NULL", account).Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease).Error; err != nil {
			return err
		}
		budget, err := lockSDKBudget(cleanup, tx, account)
		if err != nil {
			return err
		}
		var row executionmodelv1.VenueOrderIntent
		if err := tx.WithContext(cleanup).Model(&executionmodelv1.VenueOrderIntent{}).
			Where("id = ? AND delete_at IS NULL", admission.IntentID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if !s.matchesAdmission(&row, admission) || row.State != IntentDispatched || row.OrderId != "" {
			return ErrInvalidSubmit
		}
		if err := expireUnsent(cleanup, tx, budget, &row); err != nil {
			return err
		}
		return tx.WithContext(cleanup).Model(&commonmodelv1.TradingAccountPolicy{}).Where("id = ?", budget.Id).
			Select("ReservedNotional").Updates(budget).Error
	})
}
