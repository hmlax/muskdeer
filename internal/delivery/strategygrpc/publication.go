package strategygrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// PublicationServer borrows the atomic publisher without owning resources.
type PublicationServer struct {
	strategyv1.UnimplementedStrategyPublicationServer
	publisher domain.AdminStrategyPublisher
}

// NewPublication performs no I/O or implicit runtime state change.
func NewPublication(publisher domain.AdminStrategyPublisher) (*PublicationServer, error) {
	if publisher == nil {
		return nil, domain.ErrInvalidAdminPublication
	}
	return &PublicationServer{publisher: publisher}, nil
}

// PublishConfiguration persists requested input and its immutable audit receipt.
func (s *PublicationServer) PublishConfiguration(ctx context.Context, request *strategyv1.PublishConfigurationRequest) (*strategyv1.PublicationReceipt, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, controlRPCError(err)
	}
	if !controlCSRFValid(request.CsrfToken) {
		return nil, controlRPCError(domain.ErrForbidden)
	}
	command, err := strategywire.Publication(request.Input, request.StrategyId, request.IdempotencyKey)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, domain.MessageInvalidAdminPublication)
	}
	receipt, err := s.publisher.Publish(ctx, request.Session.Token, request.CsrfToken, command)
	if errors.Is(err, domain.ErrInvalidAdminPublication) {
		return nil, status.Error(codes.InvalidArgument, domain.MessageInvalidAdminPublication)
	}
	if err != nil {
		return nil, controlRPCError(err)
	}
	return &strategyv1.PublicationReceipt{
		Id:                    receipt.ID,
		ActorUserId:           receipt.ActorUserID,
		StrategyId:            receipt.StrategyID,
		PreviousConfigVersion: receipt.PreviousConfigVersion,
		ConfigVersion:         receipt.ConfigVersion,
		PreviousRevision:      receipt.PreviousRevision,
		Revision:              receipt.Revision,
		MarketDataSource:      string(receipt.Source),
		Reason:                receipt.Reason,
		CreatedAtMs:           receipt.CreatedAt.UnixMilli(),
	}, nil
}
