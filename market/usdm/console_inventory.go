package usdm

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

// DescribeInstances reports startup inputs rather than inferring configuration from database rows.
func (r *Runtime) DescribeInstances(ctx context.Context, _ *commonv1.VenueInstanceInventoryRequest) (*commonv1.VenueInstanceInventory, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, "strategy service identity required")
	}
	out := &commonv1.VenueInstanceInventory{}
	for _, item := range r.inventory {
		out.Instances = append(out.Instances, proto.CloneOf(item))
	}
	return out, nil
}
