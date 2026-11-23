package service

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
)

const (
	messageInvalidTradingAccountID = "invalid trading account identifier"
	messageAccountNotConfirmed     = "account registration was not confirmed"
)

// EnableAccount forwards the merchant's verified scope without changing limits.
func (s *MuskdeerService) EnableAccount(ctx context.Context, in *pb.MuskdeerEnableAccountRequest) (*pb.MuskdeerAccount, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.EnableAccount(ctx, &core.EnableAccountRequest{
		Actor:               actor,
		Id:                  in.GetId(),
		ExpectedUpdatedAtMs: in.GetExpectedUpdatedAtMs(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreAccount(result), nil
}

// CreateAccount enrolls only a new fund beneath an exclusively authorized SDK
// user. Ownership stays inactive until the core confirms an existing fund.
func (s *MuskdeerService) CreateAccount(ctx context.Context, in *pb.MuskdeerCreateAccountRequest) (*pb.MuskdeerAccount, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	draft := in.GetData()
	user, uok := muskdeer.TradingID(draft.GetUserId())
	fund, fok := muskdeer.TradingID(draft.GetFundId())
	if !uok || !fok {
		return nil, s.failure(ctx, status.Error(codes.InvalidArgument, messageInvalidTradingAccountID))
	}
	reservation, err := s.bindings.BeginAccountEnrollment(ctx, user, fund)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	actor.AllowedAccountIds = append(actor.AllowedAccountIds, reservation.AccountId)
	result, err := s.client.CreateAccount(ctx, &core.CreateAccountRequest{
		Actor: actor,
		Data:  toCoreAccountDraft(draft),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	if result.GetAccountId() != reservation.AccountId || result.GetUserId() != user || result.GetFundId() != fund || result.GetId() <= 0 {
		return nil, s.failure(ctx, status.Error(codes.Unavailable, messageAccountNotConfirmed))
	}
	if err := s.bindings.CompleteAccountEnrollment(ctx, reservation.Id); err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreAccount(result), nil
}
