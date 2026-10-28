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

// CreationServer owns no resource and borrows the atomic metadata creator.
type CreationServer struct {
	strategyv1.UnimplementedStrategyCreationServer
	creator domain.AdminStrategyCreator
}

// NewCreation performs no I/O or implicit account/strategy creation.
func NewCreation(creator domain.AdminStrategyCreator) (*CreationServer, error) {
	if creator == nil {
		return nil, domain.ErrInvalidAdminCreation
	}
	return &CreationServer{creator: creator}, nil
}

// CreateStrategy persists an initial definition; it never requests running.
func (s *CreationServer) CreateStrategy(ctx context.Context, request *strategyv1.CreateStrategyRequest) (*strategyv1.CreationReceipt, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, controlRPCError(err)
	}
	if !controlCSRFValid(request.CsrfToken) {
		return nil, controlRPCError(domain.ErrForbidden)
	}
	command, err := strategywire.Creation(request.Input, request.IdempotencyKey)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, domain.MessageInvalidAdminCreation)
	}
	receipt, err := s.creator.Create(ctx, request.Session.Token, request.CsrfToken, command)
	if errors.Is(err, domain.ErrInvalidAdminCreation) {
		return nil, status.Error(codes.InvalidArgument, domain.MessageInvalidAdminCreation)
	}
	if err != nil {
		return nil, controlRPCError(err)
	}
	return &strategyv1.CreationReceipt{
		Id:               receipt.ID,
		ActorUserId:      receipt.ActorUserID,
		StrategyId:       receipt.StrategyID,
		ConfigVersion:    receipt.ConfigVersion,
		Revision:         receipt.Revision,
		MarketDataSource: string(receipt.Source),
		Reason:           receipt.Reason,
		CreatedAtMs:      receipt.CreatedAt.UnixMilli(),
	}, nil
}
