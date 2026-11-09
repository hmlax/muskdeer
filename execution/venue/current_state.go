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
	// Keep the two disjoint reservation sets as separate indexed scans. One
	// statement snapshot still covers every uncertain send and every ACK newer
	// than the authoritative read began, without scanning old ACK heap pages.
	uncertain := db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&execution.VenueOrderIntent{}).
		Where("account_id = ? AND delete_at IS NULL", budget.AccountId).
		Where("state IN ?", []string{IntentPrepared, IntentDispatched}).Select("COALESCE(SUM(reserved_notional), 0)")
	recent := db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&execution.VenueOrderIntent{}).
		Where("account_id = ? AND delete_at IS NULL", budget.AccountId).
		Where("state = ? AND update_at >= ?", IntentAcknowledged, budget.SnapshotExpiresAt-native.SnapshotAge.Milliseconds()).Select("COALESCE(SUM(reserved_notional), 0)")
	err := db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&model.TradingAccountPolicy{}).
		Where("id = ?", budget.Id).Select("((?) + (?))::text AS notional", uncertain, recent).Take(&total).Error
	if err != nil {
		return decimal.Zero, err
	}
	return domain.ParseLedgerDecimal(total.Notional, false, true)
}
