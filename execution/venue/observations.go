package venue

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Observe correlates native facts and releases only terminal unfilled quantity.
// Filled gross notional remains conservatively reserved; it is not silently
// netted against an independently observed position or a later opposite fill.
func (s *Store) Observe(ctx context.Context, lease domain.LeaseToken, snapshot *commonv1.VenueSnapshot) error {
	if !validStoreContext(ctx) || snapshot.GetScope() == nil || lease.Key.AccountID != native.ScopeKey(snapshot.Scope.UserId, snapshot.Scope.FundId) {
		return ErrInvalidSnapshot
	}
	byClient := make(map[string]*commonv1.VenueOrder, len(snapshot.Orders))
	for _, order := range snapshot.Orders {
		if order == nil || order.OrderId == "" || !native.Active(order.Status) && !native.Terminal(order.Status) {
			return ErrInvalidSnapshot
		}
		if previous := byClient[order.ClientId]; previous != nil && previous.OrderId != order.OrderId && order.ClientId != "" {
			return ErrInvalidSnapshot
		}
		byClient[order.ClientId] = order
	}
	return s.db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		_, now, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch))
		if err != nil {
			return err
		}
		budget, err := lockSDKBudget(ctx, tx, lease.Key.AccountID)
		if err != nil {
			return err
		}
		var rows []*executionmodelv1.VenueOrderIntent
		if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
			Where("account_id = ? AND state != ?", lease.Key.AccountID, IntentTerminal).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			order := byClient[row.ClientId]
			if row.State == IntentPrepared && row.ExpiresAt > 0 && now >= min(row.ExpiresAt, row.SourceUntil) {
				if order != nil {
					return ErrInvalidSnapshot
				}
				if err := expireUnsent(ctx, tx, budget, row); err != nil {
					return err
				}
				continue
			}
			if order == nil {
				continue // Absence does not prove non-execution or cancellation.
			}
			if err := observeIntent(ctx, tx, budget, row, order, snapshot.Scope); err != nil {
				return err
			}
		}
		for _, order := range snapshot.Orders {
			if native.Terminal(order.Status) {
				if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).
					Where("account_id = ? AND symbol = ? AND order_id = ?", lease.Key.AccountID, snapshot.Scope.Symbol, order.OrderId).
					Update("state", CancelConfirmed).Error; err != nil {
					return err
				}
			}
		}
		return tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("id = ?", budget.Id).
			Select("ReservedNotional").Updates(budget).Error
	})
}

func expireUnsent(ctx context.Context, tx *gorm.DB, budget *commonmodelv1.TradingAccountPolicy, row *executionmodelv1.VenueOrderIntent) error {
	reserved, e1 := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	release, e2 := domain.ParseLedgerDecimal(row.ReservedNotional, false, false)
	if e1 != nil || e2 != nil || release.GreaterThan(reserved) {
		return ErrInvalidSnapshot
	}
	budget.ReservedNotional = reserved.Sub(release).String()
	row.State = IntentTerminal
	return tx.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Where("id = ?", row.Id).Select("State").Updates(row).Error
}

func observeIntent(ctx context.Context, tx *gorm.DB, budget *commonmodelv1.TradingAccountPolicy, row *executionmodelv1.VenueOrderIntent, order *commonv1.VenueOrder, scope *commonv1.VenueScope) error {
	price, e1 := native.Value(order.Price, false, false)
	quantity, e2 := native.Value(order.Quantity, false, false)
	executed, e3 := native.Value(order.ExecutedQuantity, false, true)
	expectedPrice, e4 := domain.ParseLedgerDecimal(row.Price, false, false)
	expectedQuantity, e5 := domain.ParseLedgerDecimal(row.Quantity, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || executed.GreaterThan(quantity) ||
		!price.Equal(expectedPrice) || !quantity.Equal(expectedQuantity) || order.Side != row.Side || row.Symbol != scope.Symbol ||
		row.UserId != scope.UserId || row.FundId != scope.FundId || row.OrderId != "" && row.OrderId != order.OrderId || row.State == IntentPrepared {
		return ErrInvalidSnapshot
	}
	row.OrderId, row.State = order.OrderId, IntentAcknowledged
	if native.Terminal(order.Status) {
		reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil {
			return err
		}
		release := quantity.Sub(executed).Mul(price)
		if reserved.LessThan(release) {
			return ErrInvalidSnapshot
		}
		budget.ReservedNotional = reserved.Sub(release).String()
		row.State = IntentTerminal
	}
	return tx.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Where("id = ?", row.Id).
		Select("State", "OrderId").Updates(row).Error
}

// PendingIDs includes known orders and cancellation targets requiring lookup.
func (s *Store) PendingIDs(ctx context.Context, account string) ([]uint64, error) {
	var intents []*executionmodelv1.VenueOrderIntent
	if err := s.db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
		Where("account_id = ? AND state != ? AND order_id != ''", account, IntentTerminal).Limit(257).Find(&intents).Error; err != nil {
		return nil, err
	}
	var cancels []*executionmodelv1.VenueCancellation
	query, err := pendingSDKCancellations(ctx, s.db, account)
	if err != nil {
		return nil, err
	}
	if err := query.Where("state != ?", CancelConfirmed).Limit(257).Find(&cancels).Error; err != nil {
		return nil, err
	}
	if len(intents) > 256 || len(cancels) > 256 {
		return nil, ErrRiskLimit
	}
	seen := make(map[uint64]struct{})
	var result []uint64
	add := func(text string) error {
		id, err := strconv.ParseUint(text, 10, 64)
		if err != nil || id == 0 {
			return ErrInvalidSnapshot
		}
		if _, exists := seen[id]; !exists {
			result = append(result, id)
			seen[id] = struct{}{}
		}
		return nil
	}
	for _, row := range intents {
		if err := add(row.OrderId); err != nil {
			return nil, err
		}
	}
	for _, row := range cancels {
		if err := add(row.OrderId); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// SyncBudget updates observations without enabling a disabled fund. Observed
// gross obligation never decreases automatically, preserving legacy/fill risk.
func (s *Store) SyncBudget(ctx context.Context, lease domain.LeaseToken, snapshot *commonv1.VenueSnapshot, observed decimal.Decimal) error {
	return s.db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch)); err != nil {
			return err
		}
		row, err := lockSDKBudget(ctx, tx, lease.Key.AccountID)
		if err != nil {
			return err
		}
		if snapshot.GetScope().GetUserId() != row.UserId || snapshot.Scope.FundId != row.FundId || observed.IsNegative() {
			return ErrInvalidSnapshot
		}
		previous, err := domain.ParseLedgerDecimal(row.ObservedNotional, false, true)
		if err != nil {
			return err
		}
		baseline, err := loadDevelopmentBaseline(ctx, tx, lease.Key.AccountID)
		if err != nil {
			return err
		}
		if baseline != nil {
			minimum, e1 := domain.ParseLedgerDecimal(baseline.MinimumObservedNotional, false, false)
			legacy, e2 := domain.ParseLedgerDecimal(baseline.LegacyNotional, false, false)
			if e1 != nil || e2 != nil || previous.LessThan(minimum) {
				return ErrRiskLimit
			}
			observed = observed.Add(legacy)
		}
		row.ObservedNotional = decimal.Max(previous, observed).String()
		row.AvailableQuote = snapshot.Available
		row.WorstFeeRate = snapshot.Market.WorstFeeRate
		row.SnapshotExpiresAt = snapshot.ValidUntilMs
		return tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("id = ?", row.Id).
			Select("ObservedNotional", "AvailableQuote", "WorstFeeRate", "SnapshotExpiresAt").Updates(row).Error
	})
}

// Budget reads policy only; a read is never submission authority.
func (s *Store) Budget(ctx context.Context, account string) (*commonmodelv1.TradingAccountPolicy, error) {
	var row commonmodelv1.TradingAccountPolicy
	err := s.db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", account).First(&row).Error
	return &row, err
}

// CompleteCutover is called only after a full native query confirms legacy quotes
// are gone. Subsequent legacy quote appearance blocks new risk instead of fighting another producer.
func (s *Store) CompleteCutover(ctx context.Context, lease domain.LeaseToken) error {
	return s.db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch)); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", lease.Key.AccountID).Update("cutover_completed", true).Error
	})
}
