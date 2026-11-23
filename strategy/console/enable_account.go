package console

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// EnableAccount verifies the existing fund and enables only its current policy.
// It never changes limits, starts instances or creates an SDK account.
func (s *Server) EnableAccount(ctx context.Context, in *pb.EnableAccountRequest) (*pb.Account, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	if in.GetActor().GetTenantId() == 0 {
		return nil, status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	if in.GetId() <= 0 || in.GetExpectedUpdatedAtMs() <= 0 {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	policy := &model.TradingAccountPolicy{}
	query := scoped(fresh(ctx, s.db).Model(&model.TradingAccountPolicy{}).Where("id = ? AND delete_at IS NULL", in.Id), in.Actor)
	if err := query.Take(policy).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if policy.Enabled {
		return accountDTO(policy), nil
	}
	if policy.UpdateAt != in.ExpectedUpdatedAtMs {
		return nil, status.Error(codes.Aborted, messageInvalid)
	}
	profile, err := venueconfig.ProfileFromBudget(policy)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, messageInvalid)
	}
	inspect := s.accountInspector
	if inspect == nil {
		inspect = s.inspectVenueAccount
	}
	snapshot, err := inspect(ctx, native.Scope(profile))
	if err != nil {
		return nil, err
	}
	if snapshot.GetScope().GetUserId() != policy.UserId || snapshot.GetScope().GetFundId() != policy.FundId || snapshot.GetScope().GetSymbol() != policy.Symbol || snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || snapshot.GetMarket().GetPriceTick() != profile.PriceTick || snapshot.GetMarket().GetQuantityStep() != profile.QuantityStep || policy.Leverage > snapshot.GetMarket().GetMaximumLeverage() {
		return nil, status.Error(codes.FailedPrecondition, messageInvalid)
	}
	err = fresh(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		current := &model.TradingAccountPolicy{}
		locked := scoped(fresh(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ? AND delete_at IS NULL", in.Id), in.Actor)
		if err := locked.Clauses(clause.Locking{Strength: "UPDATE"}).Take(current).Error; err != nil {
			return err
		}
		if current.Enabled {
			policy = current
			return nil
		}
		if !proto.Equal(current, policy) {
			return status.Error(codes.Aborted, messageInvalid)
		}
		if snapshot.GetValidUntilMs() <= time.Now().UnixMilli() {
			return status.Error(codes.FailedPrecondition, messageInvalid)
		}
		if err := fresh(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ?", current.Id).Updates(map[string]any{
			"enabled":   true,
			"update_at": max(time.Now().UnixMilli(), current.UpdateAt+1),
		}).Error; err != nil {
			return err
		}
		if err := fresh(ctx, tx).Model(&model.TradingAccountPolicy{}).Where("id = ?", current.Id).Take(current).Error; err != nil {
			return err
		}
		policy = current
		return nil
	})
	if err != nil {
		if status.Code(err) == codes.Aborted || status.Code(err) == codes.FailedPrecondition {
			return nil, err
		}
		return nil, s.fail(ctx, err)
	}
	return accountDTO(policy), nil
}
