package venue

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// CancelInstanceOrders uses the account gate and durable cancellation journal.
// An instance must be stopped to prevent replenishment; no process lease is used.
func (r *Instances) CancelInstanceOrders(ctx context.Context, in *pb.CancelInstanceRequest) (*pb.QuoteResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if !in.GetCancelAll() {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	child, err := r.selected(in.GetId(), in.GetRevision(), false)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := child.db.WithContext(ctx).Model(&model.StrategyInstance{}).Where("id = ? AND revision = ? AND desired_running = ? AND delete_at IS NULL", in.Id, in.Revision, false).Count(&count).Error; err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	if count != 1 {
		return nil, status.Error(codes.FailedPrecondition, "stop the instance before cancellation")
	}
	profile := child.cfg.Venue.Profiles[0]
	leave, err := child.enter(native.Scope(profile))
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	defer leave()
	if err := child.cancelManaged(ctx, profile); err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return &pb.QuoteResult{State: native.StateRecovering}, nil
}
