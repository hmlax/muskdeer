package himalaya

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

// abandonUnsent is legal only in the invocation that successfully committed the
// claim and positively knows that it never entered the SDK. A lost SDK reply or
// process crash keeps DISPATCHED. Another invocation cannot reuse the claim.
func (s *Store) abandonUnsent(ctx context.Context, admission Admission) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.db.WithContext(cleanup).Model(&modelv1.HimalayaIntent{}).Transaction(func(tx *gorm.DB) error {
		account := AccountID(admission.UserID, admission.FundID)
		var lease modelv1.ExecutionShardLease
		if err := tx.WithContext(cleanup).Model(&modelv1.ExecutionShardLease{}).
			Where("account_id = ? AND delete_at IS NULL", account).Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease).Error; err != nil {
			return err
		}
		budget, err := lockSDKBudget(cleanup, tx, account)
		if err != nil {
			return err
		}
		var row modelv1.HimalayaIntent
		if err := tx.WithContext(cleanup).Model(&modelv1.HimalayaIntent{}).
			Where("id = ? AND delete_at IS NULL", admission.IntentID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if !matchesAdmission(&row, admission) || row.State != IntentDispatched || row.OrderId != "" {
			return ErrInvalidSubmit
		}
		if err := expireUnsent(cleanup, tx, budget, &row); err != nil {
			return err
		}
		return tx.WithContext(cleanup).Model(&modelv1.HimalayaBudget{}).Where("id = ?", budget.Id).
			Select("ReservedNotional").Updates(budget).Error
	})
}
