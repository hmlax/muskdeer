package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

// A committed core fill changes inventory before portfolio delivery. Close
// admission in this same core transaction until facts are projected/reconciled.
func invalidateUnprojectedRisk(ctx context.Context, tx *gorm.DB, accountID string) error {
	var snapshots []*modelv1.RiskSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("account_id = ? AND delete_at IS NULL", accountID).
		Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&snapshots).Error; err != nil {
		return err
	}
	for _, row := range snapshots {
		previous := row.Revision
		row.Revision++
		row.Reconciled = false
		result := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ? AND revision = ?", row.Id, previous).
			Select("Reconciled", "Revision").Updates(row)
		if err := reservationResult(result); err != nil {
			return err
		}
	}
	return nil
}
