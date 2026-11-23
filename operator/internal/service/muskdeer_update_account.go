package service

import (
	"context"

	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
)

// UpdateAccount forwards parameters using only verified merchant bindings.
func (s *MuskdeerService) UpdateAccount(ctx context.Context, in *pb.MuskdeerUpdateAccountRequest) (*pb.MuskdeerAccount, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.UpdateAccount(ctx, &core.UpdateAccountRequest{
		Actor:               actor,
		Id:                  in.GetId(),
		ExpectedUpdatedAtMs: in.GetExpectedUpdatedAtMs(),
		Parameters:          toCoreAccountParameters(in.GetParameters()),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreAccount(result), nil
}
