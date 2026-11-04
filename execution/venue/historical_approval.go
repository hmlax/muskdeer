package venue

import (
	"context"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// ApproveHistoricalIntents annotates an explicit finite set of old unknown
// submissions. Original state, identity and the whole reservation remain intact.
// The maintenance caller must first obtain two stable, empty native order views.
// This method never runs automatically and never approves a later unknown.
func (s *Store) ApproveHistoricalIntents(ctx context.Context, lease domain.LeaseToken, request *commonv1.HistoricalIntentApproval, snapshot *commonv1.VenueSnapshot) error {
	return s.historicalIntents(ctx, lease, request, snapshot, true)
}

// AssessHistoricalIntents validates the same finite approval without annotating
// intents or changing financial state. The caller still owns the execution lease.
func (s *Store) AssessHistoricalIntents(ctx context.Context, lease domain.LeaseToken, request *commonv1.HistoricalIntentApproval, snapshot *commonv1.VenueSnapshot) error {
	return s.historicalIntents(ctx, lease, request, snapshot, false)
}

func (s *Store) historicalIntents(ctx context.Context, lease domain.LeaseToken, request *commonv1.HistoricalIntentApproval, snapshot *commonv1.VenueSnapshot, apply bool) error {
	ids, err := historicalApprovalIDs(request, snapshot)
	if err != nil {
		return err
	}
	if !validStoreContext(ctx) || lease.Key.Kind != domain.LeaseKindExecution || lease.Key.AccountID != AccountID(request.Scope.UserId, request.Scope.FundId) {
		return ErrInvalidSubmit
	}
	return s.db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		_, _, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch))
		if err != nil {
			return err
		}
		if !freshHistoricalSnapshot(snapshot) {
			return ErrInvalidSnapshot
		}
		budget, err := lockSDKBudget(ctx, tx, lease.Key.AccountID)
		if err != nil {
			return err
		}
		if err := checkReservedBudget(budget); err != nil {
			return err
		}
		var rows []*executionmodelv1.VenueOrderIntent
		if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
			Where("account_id = ? AND state != ? AND delete_at IS NULL", lease.Key.AccountID, IntentTerminal).
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
		found := 0
		held := decimal.Zero
		for _, row := range rows {
			amount, err := domain.ParseLedgerDecimal(row.ReservedNotional, false, false)
			if err != nil {
				return ErrRiskLimit
			}
			held = held.Add(amount)
			if row.HistoricalApprovalReference != "" && !ids[row.Id] {
				continue
			}
			if !ids[row.Id] || !matchesHistoricalApproval(row, request, snapshot) {
				return domain.ErrOutcomeUnknown
			}
			found++
			if row.HistoricalApprovalReference != "" || !apply {
				continue
			}
			row.HistoricalApprovalReference = request.ApprovalReference
			row.HistoricalSnapshot = proto.CloneOf(snapshot)
			if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Where("id = ?", row.Id).
				Select("HistoricalApprovalReference", "HistoricalSnapshot").Updates(row).Error; err != nil {
				return err
			}
		}
		if found != len(ids) {
			return domain.ErrOutcomeUnknown
		}
		reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil || reserved.LessThan(held) {
			return ErrRiskLimit
		}
		return nil
	})
}

func historicalApprovalIDs(request *commonv1.HistoricalIntentApproval, snapshot *commonv1.VenueSnapshot) (map[int64]bool, error) {
	if request.GetScope() == nil || snapshot.GetScope() == nil || request.Scope.UserId <= 0 || request.Scope.FundId <= 0 ||
		!native.Symbol(request.Scope.Symbol) || strings.TrimSpace(request.ApprovalReference) == "" || len(request.ApprovalReference) > 256 ||
		len(request.IntentIds) == 0 || len(request.IntentIds) > 256 || !proto.Equal(request.Scope, snapshot.Scope) || len(snapshot.Orders) != 0 {
		return nil, ErrInvalidSubmit
	}
	ids := make(map[int64]bool, len(request.IntentIds))
	for _, id := range request.IntentIds {
		if id <= 0 || ids[id] {
			return nil, ErrInvalidSubmit
		}
		ids[id] = true
	}
	return ids, nil
}

func freshHistoricalSnapshot(snapshot *commonv1.VenueSnapshot) bool {
	// Observation timestamps come from this caller's clock, as in Normalize
	// and the original baseline. Lease ownership uses the database clock.
	now := time.Now().UnixMilli()
	return snapshot.StartedAtMs > 0 && snapshot.StartedAtMs <= snapshot.ObservedAtMs && snapshot.ObservedAtMs <= now &&
		snapshot.ValidUntilMs > now && snapshot.ValidUntilMs-snapshot.StartedAtMs <= native.SnapshotAge.Milliseconds()
}

func matchesHistoricalApproval(row *executionmodelv1.VenueOrderIntent, request *commonv1.HistoricalIntentApproval, snapshot *commonv1.VenueSnapshot) bool {
	return row.State == IntentDispatched && row.OrderId == "" && row.Symbol == request.Scope.Symbol &&
		row.UserId == request.Scope.UserId && row.FundId == request.Scope.FundId && row.CreateAt < snapshot.StartedAtMs &&
		(row.HistoricalApprovalReference == "" || row.HistoricalApprovalReference == request.ApprovalReference)
}
