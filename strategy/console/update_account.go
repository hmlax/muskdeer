package console

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// UpdateAccount changes only the scoped account's live limits and leverage.
func (s *Server) UpdateAccount(ctx context.Context, in *pb.UpdateAccountRequest) (*pb.Account, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	if in.GetActor().GetTenantId() == 0 {
		return nil, status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	if in.GetId() <= 0 || in.GetExpectedUpdatedAtMs() <= 0 || in.GetParameters() == nil {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	var policy model.TradingAccountPolicy
	if err := scoped(fresh(ctx, s.db).Model(&model.TradingAccountPolicy{}).Where("id = ? AND delete_at IS NULL", in.Id), in.Actor).Take(&policy).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if policy.UpdateAt != in.ExpectedUpdatedAtMs {
		return nil, makerCheckError(domain.AccountPolicyConflictReason, domain.AccountPolicyConflictMessage)
	}
	profile, err := venueconfig.ProfileFromBudget(&policy)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, messageInvalid)
	}
	command := &execution.AccountPolicyUpdate{
		Id:                   policy.Id,
		ExpectedUpdatedAtMs:  in.ExpectedUpdatedAtMs,
		Scope:                native.Scope(profile),
		MaximumNotional:      in.Parameters.MaximumNotional,
		MaximumOrderNotional: in.Parameters.MaximumOrderNotional,
		Leverage:             in.Parameters.Leverage,
	}
	update := s.accountPolicyUpdater
	if update == nil {
		update = s.updateVenueAccount
	}
	result, err := update(ctx, command)
	if err != nil {
		return nil, err
	}
	if result.GetId() != policy.Id || result.GetUpdatedAtMs() < policy.UpdateAt || result.GetProfile().GetUserId() != policy.UserId || result.GetProfile().GetFundId() != policy.FundId || result.GetProfile().GetSymbol() != policy.Symbol {
		return nil, status.Error(codes.Unavailable, messageUnavailable)
	}
	policy.MaximumNotional, policy.MaximumOrderNotional, policy.Leverage = result.Profile.MaximumNotional, result.Profile.MaximumOrderNotional, result.Profile.Leverage
	policy.UpdateAt, policy.Enabled = result.UpdatedAtMs, result.Enabled
	return accountDTO(&policy), nil
}

func (s *Server) updateVenueAccount(ctx context.Context, in *execution.AccountPolicyUpdate) (*execution.AccountPolicyResult, error) {
	if s.factory == nil {
		return nil, status.Error(codes.Unavailable, messageUnavailable)
	}
	conn, err := s.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return nil, err
	}
	return execution.NewExecutionClient(conn).UpdateAccountPolicy(ctx, in)
}
