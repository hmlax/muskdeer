package himalaya

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// AssessDevelopmentBaseline is read-only and never changes cancellation proof.
func (s *Store) AssessDevelopmentBaseline(ctx context.Context, request *commonv1.DevelopmentBaselineRequest, snapshot *commonv1.HimalayaSnapshot) (*executionmodelv1.HimalayaDevelopmentBaseline, error) {
	if !validStoreContext(ctx) {
		return nil, ErrInvalidSubmit
	}
	return prepareDevelopmentBaseline(ctx, s.db, request, snapshot, false)
}

// ApplyDevelopmentBaseline is an explicit, one-time development maintenance action.
// It preserves original unknown records and permanently holds their gross risk.
func (s *Store) ApplyDevelopmentBaseline(ctx context.Context, lease domain.LeaseToken, request *commonv1.DevelopmentBaselineRequest, snapshot *commonv1.HimalayaSnapshot) error {
	if !validStoreContext(ctx) || lease.Key.AccountID != native.ScopeKey(request.GetScope().GetUserId(), request.GetScope().GetFundId()) {
		return ErrInvalidSubmit
	}
	return s.db.WithContext(ctx).Model(&executionmodelv1.HimalayaDevelopmentBaseline{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch)); err != nil {
			return err
		}
		candidate, err := prepareDevelopmentBaseline(ctx, tx, request, snapshot, true)
		if err != nil {
			return err
		}
		if candidate.Id != 0 {
			return nil
		}
		if err := tx.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).Update("observed_notional", candidate.MinimumObservedNotional).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&executionmodelv1.HimalayaDevelopmentBaseline{}).Create(candidate).Error
	})
}

func prepareDevelopmentBaseline(ctx context.Context, db *gorm.DB, request *commonv1.DevelopmentBaselineRequest, snapshot *commonv1.HimalayaSnapshot, lock bool) (*executionmodelv1.HimalayaDevelopmentBaseline, error) {
	if snapshot.GetStartedAtMs() <= 0 || snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || snapshot.ValidUntilMs-snapshot.StartedAtMs > native.SnapshotAge.Milliseconds() || len(snapshot.GetOrders()) != 0 {
		return nil, ErrInvalidSnapshot
	}
	legacy, current, err := developmentAmounts(request, snapshot)
	if err != nil {
		return nil, err
	}
	account := native.ScopeKey(request.Scope.UserId, request.Scope.FundId)
	digest, err := developmentRequestDigest(request)
	if err != nil {
		return nil, err
	}
	existing, err := loadDevelopmentBaseline(ctx, db, account)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.RequestDigest != digest {
			return nil, ErrInvalidSubmit
		}
		return existing, nil
	}
	var intents int64
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("account_id = ? AND state != ?", account, IntentTerminal).Count(&intents).Error; err != nil {
		return nil, err
	}
	if intents != 0 {
		return nil, domain.ErrOutcomeUnknown
	}
	if err := matchDevelopmentCancellations(ctx, db, account, request); err != nil {
		return nil, err
	}
	var budget *commonmodelv1.HimalayaBudget
	if lock {
		budget, err = lockSDKBudget(ctx, db, account)
	} else {
		budget, err = (&Store{db: db}).Budget(ctx, account)
	}
	if err != nil {
		return nil, err
	}
	maximum, observed, reserved, err := budgetAmounts(budget)
	if err != nil {
		return nil, err
	}
	minimum := decimal.Max(observed, legacy.Add(current))
	if !budget.Enabled || budget.UserId != request.Scope.UserId || budget.FundId != request.Scope.FundId || minimum.Add(reserved).GreaterThan(maximum) {
		return nil, ErrRiskLimit
	}
	row := &executionmodelv1.HimalayaDevelopmentBaseline{
		AccountId:               account,
		Request:                 proto.CloneOf(request),
		RequestDigest:           digest,
		Snapshot:                proto.CloneOf(snapshot),
		LegacyNotional:          legacy.String(),
		MinimumObservedNotional: minimum.String(),
	}
	row.EvidenceDigest, err = developmentEvidenceDigest(row)
	return row, err
}

func developmentAmounts(request *commonv1.DevelopmentBaselineRequest, snapshot *commonv1.HimalayaSnapshot) (decimal.Decimal, decimal.Decimal, error) {
	if request.GetBaselineId() == "" || len(request.BaselineId) > 64 || request.GetApprovalReference() == "" || len(request.ApprovalReference) > 128 ||
		request.GetScope().GetUserId() <= 0 || request.Scope.FundId <= 0 || !native.Symbol(request.Scope.Symbol) || !proto.Equal(request.Scope, snapshot.GetScope()) || len(request.OriginalOrders) == 0 || len(request.OriginalOrders) > 256 {
		return decimal.Zero, decimal.Zero, ErrInvalidSubmit
	}
	mark, err := native.Value(snapshot.GetMarket().GetMarkPrice(), false, false)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	legacy := decimal.Zero
	seen := make(map[string]bool)
	for _, order := range request.OriginalOrders {
		amount, err := developmentOrderAmount(request.Scope.Symbol, order, mark)
		if err != nil || seen[order.GetOrderId()] {
			return decimal.Zero, decimal.Zero, ErrInvalidSubmit
		}
		seen[order.OrderId] = true
		legacy = legacy.Add(amount)
	}
	current := decimal.Zero
	for _, position := range snapshot.Positions {
		quantity, err := native.Value(position.GetSignedQuantity(), true, true)
		if err != nil || position.GetSymbol() != request.Scope.Symbol {
			return decimal.Zero, decimal.Zero, ErrInvalidSnapshot
		}
		current = current.Add(quantity.Abs().Mul(mark))
	}
	return legacy, current, nil
}

func developmentOrderAmount(symbol string, order *commonv1.HimalayaOrder, mark decimal.Decimal) (decimal.Decimal, error) {
	id, err := strconv.ParseUint(order.GetOrderId(), 10, 64)
	price, e1 := native.Value(order.GetPrice(), false, false)
	quantity, e2 := native.Value(order.GetQuantity(), false, false)
	executed, e3 := native.Value(order.GetExecutedQuantity(), false, true)
	if err != nil || id == 0 || e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) || !native.Active(order.GetStatus()) ||
		!strings.HasPrefix(order.GetClientId(), native.LegacyPrefix+symbol+"_") || order.GetSide() != string(domain.SideBuy) && order.GetSide() != string(domain.SideSell) {
		return decimal.Zero, ErrInvalidSubmit
	}
	return quantity.Mul(decimal.Max(mark, price)), nil
}

func matchDevelopmentCancellations(ctx context.Context, db *gorm.DB, account string, request *commonv1.DevelopmentBaselineRequest) error {
	var rows []*executionmodelv1.HimalayaCancellation
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaCancellation{}).Where("account_id = ? AND state != ?", account, CancelConfirmed).Limit(257).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 || len(rows) > 256 {
		return ErrInvalidSubmit
	}
	expected := make(map[string]bool)
	for _, order := range request.OriginalOrders {
		expected[order.OrderId] = true
	}
	found := make(map[string]bool)
	for _, row := range rows {
		if row.State != IntentDispatched || row.UserId != request.Scope.UserId || row.FundId != request.Scope.FundId || row.Symbol != request.Scope.Symbol || !expected[row.OrderId] || row.DeleteAt != nil {
			return ErrInvalidSubmit
		}
		found[row.OrderId] = true
	}
	if len(found) != len(expected) {
		return ErrInvalidSubmit
	}
	return nil
}

func loadDevelopmentBaseline(ctx context.Context, db *gorm.DB, account string) (*executionmodelv1.HimalayaDevelopmentBaseline, error) {
	var row executionmodelv1.HimalayaDevelopmentBaseline
	err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaDevelopmentBaseline{}).Where("account_id = ?", account).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	digest, err := developmentRequestDigest(row.Request)
	if err != nil {
		return nil, err
	}
	evidence, err := developmentEvidenceDigest(&row)
	if err != nil {
		return nil, err
	}
	legacy, current, err := developmentAmounts(row.Request, row.Snapshot)
	if err != nil {
		return nil, err
	}
	storedLegacy, e1 := domain.ParseLedgerDecimal(row.LegacyNotional, false, false)
	minimum, e2 := domain.ParseLedgerDecimal(row.MinimumObservedNotional, false, false)
	if row.DeleteAt != nil || row.AccountId != native.ScopeKey(row.Request.Scope.UserId, row.Request.Scope.FundId) || digest != row.RequestDigest || evidence != row.EvidenceDigest || e1 != nil || e2 != nil || !legacy.Equal(storedLegacy) || minimum.LessThan(legacy.Add(current)) {
		return nil, ErrInvalidSubmit
	}
	return &row, nil
}

func developmentRequestDigest(request *commonv1.DevelopmentBaselineRequest) (string, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), err
}

func developmentEvidenceDigest(row *executionmodelv1.HimalayaDevelopmentBaseline) (string, error) {
	evidence := proto.CloneOf(row)
	evidence.Id, evidence.CreateAt, evidence.UpdateAt = 0, 0, 0
	evidence.DeleteAt = nil
	evidence.EvidenceDigest = ""
	legacy, e1 := domain.ParseLedgerDecimal(evidence.LegacyNotional, false, false)
	minimum, e2 := domain.ParseLedgerDecimal(evidence.MinimumObservedNotional, false, false)
	if e1 != nil || e2 != nil {
		return "", ErrInvalidSubmit
	}
	evidence.LegacyNotional, evidence.MinimumObservedNotional = legacy.String(), minimum.String()
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(evidence)
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), err
}

func pendingSDKCancellations(ctx context.Context, db *gorm.DB, account string) (*gorm.DB, error) {
	query := db.WithContext(ctx).Model(&executionmodelv1.HimalayaCancellation{}).Where("account_id = ?", account)
	baseline, err := loadDevelopmentBaseline(ctx, db, account)
	if err != nil {
		return nil, err
	}
	if baseline == nil {
		return query, nil
	}
	budget, err := (&Store{db: db}).Budget(ctx, account)
	if err != nil {
		return nil, err
	}
	observed, e1 := domain.ParseLedgerDecimal(budget.ObservedNotional, false, true)
	minimum, e2 := domain.ParseLedgerDecimal(baseline.MinimumObservedNotional, false, false)
	if e1 != nil || e2 != nil || observed.LessThan(minimum) {
		return nil, ErrRiskLimit
	}
	ids := make([]string, 0, len(baseline.Request.OriginalOrders))
	for _, order := range baseline.Request.OriginalOrders {
		ids = append(ids, order.OrderId)
	}
	return query.Where("order_id NOT IN ?", ids), nil
}

func checkDevelopmentRiskHold(ctx context.Context, db *gorm.DB, budget *commonmodelv1.HimalayaBudget) error {
	baseline, err := loadDevelopmentBaseline(ctx, db, budget.AccountId)
	if err != nil || baseline == nil {
		return err
	}
	observed, e1 := domain.ParseLedgerDecimal(budget.ObservedNotional, false, true)
	minimum, e2 := domain.ParseLedgerDecimal(baseline.MinimumObservedNotional, false, false)
	if e1 != nil || e2 != nil || observed.LessThan(minimum) {
		return ErrRiskLimit
	}
	return nil
}
