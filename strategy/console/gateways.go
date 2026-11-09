package console

import (
	"context"
	"strings"

	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	gatewayProtocol         = "grpc"
	messagePlatformRequired = "platform context required for gateway infrastructure"
)

// ListGateways describes the loaded infrastructure connection without credentials.
// Changes continue to use explicit Nacos infrastructure configuration and restart.
func (s *Server) ListGateways(ctx context.Context, in *pb.ActorRequest) (*pb.GatewayPage, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	if in.GetActor().GetTenantId() != 0 {
		return nil, status.Error(codes.PermissionDenied, messagePlatformRequired)
	}
	return &pb.GatewayPage{
		Total: 1,
		Items: []*pb.Gateway{{
			Key:            native.Venue,
			ExecutionVenue: native.Venue,
			Protocol:       gatewayProtocol,
			Target:         s.connection.GetTarget(),
			Configured:     strings.TrimSpace(s.connection.GetTarget()) != "",
		}},
	}, nil
}
