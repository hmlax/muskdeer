package venue

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// admissionReservation separates the append-only request audit from current
// venue exposure. Unknown sends stay fully reserved without freezing a fund.
// ACKs newer than the current read began still need coverage until another read.
// Normalize defines validity as read-start + SnapshotAge, not read-completion.
// The caller holds the fund row lock, shared by every admission in that fund.
func admissionReservation(ctx context.Context, db *gorm.DB, budget *model.TradingAccountPolicy, instanceID int64) (decimal.Decimal, error) {
	if instanceID == 0 {
		return domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	}
	var total struct{ Notional string }
	err := db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).
		Where("account_id = ? AND delete_at IS NULL", budget.AccountId).
		Where("state IN ? OR (state = ? AND update_at >= ?)", []string{IntentPrepared, IntentDispatched}, IntentAcknowledged, budget.SnapshotExpiresAt-native.SnapshotAge.Milliseconds()).
		Select("COALESCE(SUM(reserved_notional), 0)::text AS notional").Take(&total).Error
	if err != nil {
		return decimal.Zero, err
	}
	return domain.ParseLedgerDecimal(total.Notional, false, true)
}
