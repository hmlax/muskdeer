package venue

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// InspectAccount reads an existing fund for enrollment. It never trades,
// creates an SDK account, persists an observation, or requires a running strategy.
func (r *Instances) InspectAccount(ctx context.Context, scope *common.VenueScope) (*common.VenueSnapshot, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if scope.GetUserId() <= 0 || scope.GetFundId() <= 0 || !native.Symbol(scope.GetSymbol()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if r.client == nil || r.closing == nil || r.closing.Load() || r.client.Load() == nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	client, ok := r.client.Load().Client.(gateway.LongGridClient)
	if !ok {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	observation, err := client.ReadGrid(ctx, gateway.Scope{
		UserID: scope.UserId,
		FundID: scope.FundId,
		Symbol: scope.Symbol,
	})
	if err != nil {
		return nil, err
	}
	if observation == nil || observation.Snapshot == nil || !proto.Equal(observation.Snapshot.Scope, scope) {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return proto.CloneOf(observation.Snapshot), nil
}
