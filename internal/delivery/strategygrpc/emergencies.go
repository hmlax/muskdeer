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

// EmergencyQueryServer borrows current-viewer reads without advancing work.
type EmergencyQueryServer struct {
	strategyv1.UnimplementedStrategyEmergencyCancellationQueryServer
	reader domain.AdminEmergencyReader
}

// EmergencyControlServer accepts scoped work without sending trading commands.
type EmergencyControlServer struct {
	strategyv1.UnimplementedStrategyEmergencyCancellationControlServer
	writer domain.AdminEmergencyWriter
}

// NewEmergencyQuery opens no resources and grants no execution authority.
func NewEmergencyQuery(reader domain.AdminEmergencyReader) (*EmergencyQueryServer, error) {
	if reader == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &EmergencyQueryServer{reader: reader}, nil
}

// NewEmergencyControl borrows the atomic authorization/acceptance transaction.
func NewEmergencyControl(writer domain.AdminEmergencyWriter) (*EmergencyControlServer, error) {
	if writer == nil {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	return &EmergencyControlServer{writer: writer}, nil
}

// RequestEmergencyCancellation returns immutable acceptance, never completion.
func (s *EmergencyControlServer) RequestEmergencyCancellation(ctx context.Context, request *strategyv1.RequestEmergencyCancellationRequest) (*strategyv1.EmergencyCancellationAcceptance, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(ctx, request.Session); err != nil {
		return nil, emergencyRPCError(err)
	}
	if !controlCSRFValid(ctx, request.CsrfToken) {
		return nil, emergencyRPCError(domain.ErrForbidden)
	}
	command, err := strategywire.EmergencyCommand(request.Input, request.StrategyId, request.IdempotencyKey)
	if err != nil {
		return nil, emergencyRPCError(err)
	}
	value, err := s.writer.Request(ctx, request.GetSession().GetToken(), request.CsrfToken, command)
	if err != nil {
		return nil, emergencyRPCError(err)
	}
	reply, err := emergencyAcceptanceProto(value)
	if err != nil || !strategywire.ValidEmergencyResult(reply, command) {
		return nil, emergencyRPCError(domain.ErrAdminControlUnavailable)
	}
	return reply, nil
}

// GetEmergencyCancellation samples stored proof and current target facts.
func (s *EmergencyQueryServer) GetEmergencyCancellation(ctx context.Context, request *strategyv1.GetEmergencyCancellationRequest) (*strategyv1.EmergencyCancellationObservation, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(ctx, request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 || request.CancellationId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	value, err := s.reader.Observation(ctx, request.GetSession().GetToken(), request.StrategyId, request.CancellationId)
	if err != nil {
		return nil, rpcError(err)
	}
	reply, err := emergencyObservationProto(value)
	if err != nil || !strategywire.ValidEmergencyObservation(reply, request.StrategyId, request.CancellationId) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// ListEmergencyCancellations reads a bounded scoped history without execution.
func (s *EmergencyQueryServer) ListEmergencyCancellations(ctx context.Context, request *strategyv1.ListEmergencyCancellationsRequest) (*strategyv1.ListEmergencyCancellationsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(ctx, request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 || !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	page := domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	}
	value, err := s.reader.History(ctx, request.GetSession().GetToken(), request.StrategyId, page)
	if err != nil {
		return nil, rpcError(err)
	}
	reply, err := emergencyHistoryProto(value, page.Limit)
	if err != nil || !strategywire.ValidEmergencyHistory(reply, request.StrategyId, page) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

func emergencyRPCError(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminEmergencyCancel) {
		return status.Error(codes.InvalidArgument, domain.MessageInvalidAdminEmergencyCancel)
	}
	return controlRPCError(err)
}

var (
	_ strategyv1.StrategyEmergencyCancellationQueryServer   = (*EmergencyQueryServer)(nil)
	_ strategyv1.StrategyEmergencyCancellationControlServer = (*EmergencyControlServer)(nil)
)
