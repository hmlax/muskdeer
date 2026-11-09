package venue

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Only correlate positive facts returned in this observation. There is no
// historical pending scan, reservation release, or missing-order inference.
func (s *Store) observeMatching(ctx context.Context, snapshot *common.VenueSnapshot) error {
	if !validStoreContext(ctx) || snapshot.GetScope() == nil {
		return ErrInvalidSnapshot
	}
	account := native.ScopeKey(snapshot.Scope.UserId, snapshot.Scope.FundId)
	orders := make(map[string]*common.VenueOrder)
	clients := make([]string, 0, len(snapshot.Orders))
	for _, order := range snapshot.Orders {
		if order == nil || order.OrderId == "" || !native.Active(order.Status) && !native.Terminal(order.Status) {
			return ErrInvalidSnapshot
		}
		if previous := orders[order.ClientId]; previous != nil && previous.OrderId != order.OrderId && order.ClientId != "" {
			return ErrInvalidSnapshot
		}
		if order.ClientId != "" {
			orders[order.ClientId] = order
			clients = append(clients, order.ClientId)
		}
	}
	if len(clients) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		var rows []*execution.VenueOrderIntent
		if err := tx.WithContext(ctx).Model(&execution.VenueOrderIntent{}).
			Where("account_id = ? AND symbol = ? AND client_id IN ? AND state != ? AND owner_epoch = 0 AND delete_at IS NULL", account, snapshot.Scope.Symbol, clients, IntentTerminal).
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := observeIntent(ctx, tx, nil, row, orders[row.ClientId], snapshot.Scope); err != nil {
				return err
			}
		}
		for _, order := range snapshot.Orders {
			if native.Terminal(order.Status) {
				if err := tx.WithContext(ctx).Model(&execution.VenueCancellation{}).
					Where("account_id = ? AND symbol = ? AND order_id = ? AND state != ?", account, snapshot.Scope.Symbol, order.OrderId, CancelConfirmed).
					Update("state", CancelConfirmed).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Store) syncMatchingBudget(ctx context.Context, snapshot *common.VenueSnapshot, observed decimal.Decimal) error {
	now := time.Now().UnixMilli()
	if !validStoreContext(ctx) || snapshot.GetScope() == nil || snapshot.GetMarket() == nil || observed.IsNegative() ||
		snapshot.StartedAtMs <= 0 || snapshot.StartedAtMs > now || snapshot.ObservedAtMs < snapshot.StartedAtMs || snapshot.ObservedAtMs > now ||
		snapshot.ValidUntilMs != snapshot.StartedAtMs+native.SnapshotAge.Milliseconds() || snapshot.ValidUntilMs <= now {
		return ErrInvalidSnapshot
	}
	row := &model.TradingAccountPolicy{
		ObservedNotional:  observed.String(),
		AvailableQuote:    snapshot.Available,
		WorstFeeRate:      snapshot.Market.WorstFeeRate,
		SnapshotExpiresAt: snapshot.ValidUntilMs,
	}
	result := s.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).
		Where("account_id = ? AND user_id = ? AND fund_id = ? AND symbol = ? AND delete_at IS NULL", native.ScopeKey(snapshot.Scope.UserId, snapshot.Scope.FundId), snapshot.Scope.UserId, snapshot.Scope.FundId, snapshot.Scope.Symbol).
		Select("ObservedNotional", "AvailableQuote", "WorstFeeRate", "SnapshotExpiresAt").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInvalidSnapshot
	}
	return nil
}
