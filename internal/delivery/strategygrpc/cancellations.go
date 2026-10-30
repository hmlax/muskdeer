package strategygrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// CancellationQueryServer borrows current-viewer result reads without effects.
type CancellationQueryServer struct {
	strategyv1.UnimplementedStrategyOrderCancellationQueryServer
	reader domain.AdminCancellationReader
}

// CancellationControlServer accepts explicit audited requests with write opt-in.
type CancellationControlServer struct {
	strategyv1.UnimplementedStrategyOrderCancellationControlServer
	writer domain.AdminCancellationWriter
}

// NewCancellationQuery owns no resource, permission or trading worker.
func NewCancellationQuery(reader domain.AdminCancellationReader) (*CancellationQueryServer, error) {
	if reader == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &CancellationQueryServer{reader: reader}, nil
}

// NewCancellationControl does not dispatch; authorization stays transactional.
func NewCancellationControl(writer domain.AdminCancellationWriter) (*CancellationControlServer, error) {
	if writer == nil {
		return nil, domain.ErrInvalidAdminCancellation
	}
	return &CancellationControlServer{writer: writer}, nil
}

// RequestOrderCancellation saves one confirmed order request, not a bulk intent.
func (s *CancellationControlServer) RequestOrderCancellation(ctx context.Context, request *strategyv1.RequestOrderCancellationRequest) (*strategyv1.OrderCancellationReceipt, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, cancellationRPCError(err)
	}
	if !controlCSRFValid(request.CsrfToken) {
		return nil, cancellationRPCError(domain.ErrForbidden)
	}
	command, err := strategywire.CancellationCommand(request.Input, request.StrategyId, request.OrderId, request.IdempotencyKey)
	if err != nil {
		return nil, cancellationRPCError(err)
	}
	value, err := s.writer.Request(ctx, request.Session.Token, request.CsrfToken, command)
	if err != nil {
		return nil, cancellationRPCError(err)
	}
	reply := cancellationProto(value)
	if !strategywire.ValidCancellationResult(reply, command) {
		return nil, cancellationRPCError(domain.ErrAdminControlUnavailable)
	}
	return reply, nil
}

// GetOrderCancellation reads one persisted scoped result with no observation write.
func (s *CancellationQueryServer) GetOrderCancellation(ctx context.Context, request *strategyv1.GetOrderCancellationRequest) (*strategyv1.OrderCancellationReceipt, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 || request.CancellationId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	value, err := s.reader.Receipt(ctx, request.Session.Token, request.StrategyId, request.CancellationId)
	if err != nil {
		return nil, rpcError(err)
	}
	reply := cancellationProto(value)
	if !strategywire.ValidCancellationReceipt(reply, request.StrategyId, request.CancellationId) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

func cancellationProto(value domain.AdminCancellationReceipt) *strategyv1.OrderCancellationReceipt {
	reply := &strategyv1.OrderCancellationReceipt{
		Id:                    value.ID,
		ActorUserId:           value.ActorUserID,
		StrategyId:            value.StrategyID,
		OrderId:               value.OrderID,
		ExpectedOrderRevision: value.ExpectedOrderRevision,
		Reason:                value.Reason,
		State:                 string(value.State),
		OutcomeCode:           value.Outcome,
		Revision:              value.Revision,
		CreatedAtMs:           value.CreatedAt.UnixMilli(),
	}
	if !value.CompletedAt.IsZero() {
		reply.CompletedAtMs = proto.Int64(value.CompletedAt.UnixMilli())
	}
	if value.ObservedOrderRevision != 0 || value.ObservedOrderState != "" {
		reply.Observation = &strategyv1.OrderCancellationObservation{
			OrderRevision: value.ObservedOrderRevision,
			State:         string(value.ObservedOrderState),
		}
	}
	return reply
}

func cancellationRPCError(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminCancellation) {
		return status.Error(codes.InvalidArgument, domain.MessageInvalidAdminCancellation)
	}
	return controlRPCError(err)
}

var (
	_ strategyv1.StrategyOrderCancellationQueryServer   = (*CancellationQueryServer)(nil)
	_ strategyv1.StrategyOrderCancellationControlServer = (*CancellationControlServer)(nil)
)
