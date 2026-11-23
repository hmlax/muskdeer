package service

import (
	"context"

	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
)

// CheckInstance preserves the authenticated merchant account scope.
func (s *MuskdeerService) CheckInstance(ctx context.Context, in *pb.MuskdeerInstanceRequest) (*pb.MuskdeerMakerCheck, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.CheckInstance(ctx, &core.InstanceRequest{
		Actor: actor,
		Id:    in.GetId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreMakerCheck(result), nil
}
