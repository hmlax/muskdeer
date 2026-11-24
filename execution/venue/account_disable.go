package venue

import (
	"context"
	"errors"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// DisableAccountPolicy drains the current fund writer before disabling future
// order admission. It makes no SDK calls and never cancels existing orders.
func (r *Instances) DisableAccountPolicy(ctx context.Context, in *pb.AccountPolicyDisable) (*pb.AccountPolicyResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if in.GetId() <= 0 || in.GetExpectedUpdatedAtMs() <= 0 || in.GetScope().GetUserId() <= 0 || in.GetScope().GetFundId() <= 0 || !native.Symbol(in.GetScope().GetSymbol()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if r.store == nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	gate := r.accountPolicyGate(in.Scope)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	var result *pb.AccountPolicyResult
	err := policyDB(ctx, r.store.db).Transaction(func(tx *gorm.DB) error {
		var current model.TradingAccountPolicy
		err := policyDB(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ? AND account_id = ? AND symbol = ? AND delete_at IS NULL", in.Id, AccountID(in.Scope.UserId, in.Scope.FundId), in.Scope.Symbol).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error
		if err != nil {
			return err
		}
		if current.UpdateAt != in.ExpectedUpdatedAtMs || current.UpdateAt == math.MaxInt64 {
			return policyConflict()
		}
		profile, err := venueconfig.ProfileFromBudget(&current)
		if err != nil {
			return status.Error(codes.FailedPrecondition, native.MessageInvalid)
		}
		updated := current.UpdateAt
		if current.Enabled {
			updated = max(time.Now().UnixMilli(), current.UpdateAt+1)
			if err := policyDB(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ?", current.Id).Updates(map[string]any{
				"enabled":   false,
				"update_at": updated,
			}).Error; err != nil {
				return err
			}
		}
		result = &pb.AccountPolicyResult{
			Id:          current.Id,
			UpdatedAtMs: updated,
			Enabled:     false,
			Profile:     profile,
		}
		return nil
	})
	if err != nil {
		return nil, accountDisableError(err)
	}
	return result, nil
}

func accountDisableError(err error) error {
	if status.Code(err) != codes.Unknown {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Error(codes.NotFound, native.MessageInvalid)
	}
	return status.Error(codes.Unavailable, native.MessageInvalid)
}
