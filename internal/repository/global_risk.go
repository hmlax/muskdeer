package repository

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// The account/strategy budget locks precede these cross-account rows. Every
// operation touches one account and takes global rows in the same ID order.
func globalRiskBudgets(ctx context.Context, tx *gorm.DB, instrumentID int64, asset, strength string) ([]*modelv1.RiskBudget, error) {
	var rows []*modelv1.RiskBudget
	err := tx.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("asset = ? AND delete_at IS NULL AND ((scope_kind = ? AND scope_key = ?) OR (scope_kind = ? AND scope_key = ?))", asset, string(domain.RiskScopePlatform), asset, string(domain.RiskScopeInstrument), strconv.FormatInt(instrumentID, 10)).Order("id ASC").Clauses(clause.Locking{Strength: strength}).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 2 {
		return nil, domain.ErrRiskDenied
	}
	return rows, nil
}

func changeGlobalRiskReservation(ctx context.Context, tx *gorm.DB, instrumentID int64, asset string, delta decimal.Decimal) error {
	if !domain.FixedDecimal(delta.Abs(), 40, 36, false) {
		return domain.ErrInvalidOrder
	}
	rows, err := globalRiskBudgets(ctx, tx, instrumentID, asset, "UPDATE")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if delta.IsPositive() {
			maximum, err := domain.ParseLedgerDecimal(row.MaximumOrderNotional, false, false)
			if err != nil {
				return err
			}
			if !row.Enabled || delta.GreaterThan(maximum) {
				return domain.ErrRiskDenied
			}
		}
		reserved, err := reserveWithinLimit(row.ReservedNotional, row.LimitNotional, delta)
		if err != nil {
			return err
		}
		previous := row.Revision
		row.ReservedNotional = reserved.String()
		row.Revision++
		result := tx.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("id = ? AND revision = ?", row.Id, previous).Select("ReservedNotional", "Revision").Updates(row)
		if err := reservationResult(result); err != nil {
			return err
		}
	}
	return nil
}
