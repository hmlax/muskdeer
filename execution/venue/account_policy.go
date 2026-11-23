package venue

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// UpdateAccountPolicy serializes risk-policy updates with the existing fund writer.
// A successful response follows commit; no SDK operation occurs inside the DB transaction.
func (r *Instances) UpdateAccountPolicy(ctx context.Context, in *pb.AccountPolicyUpdate) (*pb.AccountPolicyResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if in.GetId() <= 0 || in.GetExpectedUpdatedAtMs() <= 0 || in.GetScope().GetUserId() <= 0 || in.GetScope().GetFundId() <= 0 || !native.Symbol(in.GetScope().GetSymbol()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	gate := r.accountPolicyGate(in.Scope)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, status.Error(codes.DeadlineExceeded, native.MessageInvalid)
	}
	return r.updateAccountPolicy(ctx, in)
}

func (r *Instances) accountPolicyGate(scope *common.VenueScope) chan struct{} {
	key := AccountID(scope.UserId, scope.FundId)
	for _, child := range r.ordered {
		if gate := child.gates[key]; gate != nil {
			return gate
		}
	}
	// Funds without loaded instances cannot send quotes. Concurrent management
	// commands still share a bounded operation slot until the next process reload.
	gate, _ := r.policyGates.LoadOrStore(key, make(chan struct{}, 1))
	return gate.(chan struct{})
}

func (r *Instances) updateAccountPolicy(ctx context.Context, in *pb.AccountPolicyUpdate) (*pb.AccountPolicyResult, error) {
	if r.store == nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	var observed model.TradingAccountPolicy
	if err := policyDB(ctx, r.store.db).Model(&model.TradingAccountPolicy{}).Where("id = ? AND account_id = ? AND symbol = ? AND delete_at IS NULL", in.Id, AccountID(in.Scope.UserId, in.Scope.FundId), in.Scope.Symbol).Take(&observed).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, native.MessageInvalid)
		}
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	if observed.UpdateAt != in.ExpectedUpdatedAtMs {
		return nil, policyConflict()
	}
	if _, err := proposedAccountProfile(&observed, in); err != nil {
		return nil, err
	}
	snapshot, err := r.InspectAccount(ctx, in.Scope)
	if err != nil {
		return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
	}
	candidate, err := validateAccountPolicy(&observed, in, snapshot, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	var result *pb.AccountPolicyResult
	err = policyDB(ctx, r.store.db).Transaction(func(tx *gorm.DB) error {
		var current model.TradingAccountPolicy
		if err := policyDB(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ? AND delete_at IS NULL", in.Id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; err != nil {
			return err
		}
		if !proto.Equal(&current, &observed) || current.UpdateAt == math.MaxInt64 {
			return policyConflict()
		}
		if snapshot.GetValidUntilMs() <= time.Now().UnixMilli() {
			return policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
		}
		if err := validateAccountGridLimits(ctx, tx, current.AccountId, candidate); err != nil {
			return err
		}
		updated := current.UpdateAt
		changed := current.Leverage != candidate.Leverage || !samePolicyAmount(current.MaximumNotional, candidate.MaximumNotional) || !samePolicyAmount(current.MaximumOrderNotional, candidate.MaximumOrderNotional)
		if changed {
			updated = max(time.Now().UnixMilli(), current.UpdateAt+1)
			if err := policyDB(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ?", current.Id).Updates(map[string]any{
				"maximum_notional":       candidate.MaximumNotional,
				"maximum_order_notional": candidate.MaximumOrderNotional,
				"leverage":               candidate.Leverage,
				"update_at":              updated,
			}).Error; err != nil {
				return err
			}
		}
		result = &pb.AccountPolicyResult{
			Id:          current.Id,
			UpdatedAtMs: updated,
			Enabled:     current.Enabled,
			Profile:     candidate,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func proposedAccountProfile(current *model.TradingAccountPolicy, in *pb.AccountPolicyUpdate) (*common.VenueProfilePolicy, error) {
	candidate := proto.CloneOf(current)
	candidate.MaximumNotional, candidate.MaximumOrderNotional, candidate.Leverage = in.GetMaximumNotional(), in.GetMaximumOrderNotional(), in.GetLeverage()
	profile, err := venueconfig.ProfileFromBudget(candidate)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	return profile, nil
}

func validateAccountPolicy(current *model.TradingAccountPolicy, in *pb.AccountPolicyUpdate, snapshot *common.VenueSnapshot, now int64) (*common.VenueProfilePolicy, error) {
	if current.Id != in.GetId() || current.UpdateAt != in.GetExpectedUpdatedAtMs() {
		return nil, policyConflict()
	}
	profile, err := proposedAccountProfile(current, in)
	if err != nil {
		return nil, err
	}
	matching := proto.Equal(snapshot.GetScope(), in.GetScope()) && proto.Equal(native.Scope(profile), in.GetScope())
	market := snapshot.GetMarket()
	rules := market.GetPriceTick() == profile.PriceTick && market.GetQuantityStep() == profile.QuantityStep && profile.Leverage <= market.GetMaximumLeverage()
	if !matching || !rules || snapshot.GetValidUntilMs() <= now {
		return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
	}
	for _, position := range snapshot.Positions {
		quantity, err := native.Value(position.SignedQuantity, true, true)
		if err != nil {
			return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
		}
		if !quantity.IsZero() && position.Symbol != profile.Symbol {
			return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
		}
		if !quantity.IsZero() && profile.Leverage != current.Leverage {
			return nil, policyFailure(domain.AccountPolicyPositionReason, domain.AccountPolicyPositionMessage)
		}
	}
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		if profile.Leverage != current.Leverage {
			return nil, policyFailure(domain.AccountPolicyOrdersReason, domain.AccountPolicyOrdersMessage)
		}
		_, e1 := native.Value(order.Price, false, false)
		quantity, e2 := native.Value(order.Quantity, false, false)
		executed, e3 := native.Value(order.ExecutedQuantity, false, true)
		if e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) {
			return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
		}
	}
	gross, err := snapshotGross(snapshot, nil)
	if err != nil {
		return nil, policyFailure(domain.AccountPolicySnapshotReason, domain.AccountPolicySnapshotMessage)
	}
	maximum, _ := native.Value(profile.MaximumNotional, false, false)
	if maximum.LessThan(gross) {
		return nil, policyFailure(domain.AccountPolicyExposureReason, domain.AccountPolicyExposureMessage)
	}
	return profile, nil
}

func validateAccountGridLimits(ctx context.Context, tx *gorm.DB, account string, profile *common.VenueProfilePolicy) error {
	var rows []*strategy.StrategyInstance
	if err := policyDB(ctx, tx).Model(&strategy.StrategyInstance{}).Where("account_id = ? AND delete_at IS NULL", account).Find(&rows).Error; err != nil {
		return err
	}
	maximum, _ := native.Value(profile.MaximumNotional, false, false)
	order, _ := native.Value(profile.MaximumOrderNotional, false, false)
	for _, row := range rows {
		grid := row.Parameters.GetLongGrid()
		if grid == nil {
			continue
		}
		required, e1 := native.Value(grid.OrderNotional, false, false)
		exposure, e2 := native.Value(grid.MaximumPositionNotional, false, false)
		if e1 != nil || e2 != nil || required.GreaterThan(order) || exposure.GreaterThan(maximum) {
			return policyFailure(domain.AccountPolicyGridReason, domain.AccountPolicyGridMessage)
		}
	}
	return nil
}

func samePolicyAmount(left, right string) bool {
	a, e1 := decimal.NewFromString(left)
	b, e2 := decimal.NewFromString(right)
	return e1 == nil && e2 == nil && a.Equal(b)
}

func policyDB(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}

func policyFailure(reason, message string) error {
	failure, err := status.New(codes.FailedPrecondition, message).WithDetails(&errdetails.ErrorInfo{Reason: reason})
	if err != nil {
		return status.Error(codes.FailedPrecondition, message)
	}
	return failure.Err()
}

func policyConflict() error {
	failure, err := status.New(codes.Aborted, domain.AccountPolicyConflictMessage).WithDetails(&errdetails.ErrorInfo{Reason: domain.AccountPolicyConflictReason})
	if err != nil {
		return status.Error(codes.Aborted, domain.AccountPolicyConflictMessage)
	}
	return failure.Err()
}
