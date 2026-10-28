package strategygrpc

import (
	"context"
	"encoding/base64"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// ControlServer borrows persistent control/result capabilities, owning no task.
type ControlServer struct {
	strategyv1.UnimplementedStrategyControlServer
	controls domain.AdminStrategyControl
}

// NewControl performs no I/O and grants no operator or runtime authority.
func NewControl(controls domain.AdminStrategyControl) (*ControlServer, error) {
	if controls == nil {
		return nil, domain.ErrInvalidAdminControl
	}
	return &ControlServer{controls: controls}, nil
}

// ControlStrategy accepts audited desired intent; acceptance is not completion.
func (s *ControlServer) ControlStrategy(ctx context.Context, request *strategyv1.ControlStrategyRequest) (*strategyv1.StrategyOperation, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, controlRPCError(err)
	}
	if !controlCSRFValid(request.CsrfToken) {
		return nil, controlRPCError(domain.ErrForbidden)
	}
	command, err := (domain.AdminControlRequest{
		Key:              request.IdempotencyKey,
		StrategyID:       request.StrategyId,
		ExpectedRevision: request.ExpectedRevision,
		Action:           domain.AdminControlAction(request.Action),
		Reason:           request.Reason,
	}).Normalize()
	if err != nil {
		return nil, controlRPCError(err)
	}
	operation, err := s.controls.Control(ctx, request.Session.Token, request.CsrfToken, command)
	if err != nil {
		return nil, controlRPCError(err)
	}
	return operationProto(operation), nil
}

// GetOperation only reads a durable result after persistent viewer validation.
func (s *ControlServer) GetOperation(ctx context.Context, request *strategyv1.GetOperationRequest) (*strategyv1.StrategyOperation, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if request.OperationId <= 0 {
		return nil, controlRPCError(domain.ErrInvalidAdminControl)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, controlRPCError(err)
	}
	operation, err := s.controls.Operation(ctx, request.Session.Token, request.OperationId)
	if err != nil {
		return nil, controlRPCError(err)
	}
	return operationProto(operation), nil
}

func controlCSRFValid(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func operationProto(operation domain.AdminOperation) *strategyv1.StrategyOperation {
	result := &strategyv1.StrategyOperation{
		Id:               operation.ID,
		ActorUserId:      operation.ActorUserID,
		StrategyId:       operation.StrategyID,
		Action:           string(operation.Action),
		TargetState:      string(operation.Target),
		ExpectedRevision: operation.ExpectedRevision,
		EffectRevision:   operation.EffectRevision,
		ConfigVersion:    operation.ConfigVersion,
		MarketDataSource: string(operation.Source),
		State:            string(operation.State),
		OutcomeCode:      operation.Outcome,
		Reason:           operation.Reason,
		Revision:         operation.Revision,
		CreatedAtMs:      operation.CreatedAt.UnixMilli(),
	}
	if !operation.CompletedAt.IsZero() {
		result.CompletedAtMs = proto.Int64(operation.CompletedAt.UnixMilli())
	}
	if proof := operation.Observation; proof != nil {
		result.Observation = &strategyv1.OperationObservation{
			StrategyRevision:  proof.StrategyRevision,
			State:             string(proof.State),
			RuntimeOwnerId:    proof.RuntimeOwnerID,
			RuntimeOwnerEpoch: proof.RuntimeOwnerEpoch,
		}
	}
	return result
}

func controlRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrAdminConflict):
		return status.Error(codes.Aborted, domain.MessageAdminConflict)
	case errors.Is(err, domain.ErrInvalidAdminControl):
		return status.Error(codes.InvalidArgument, domain.MessageInvalidAdminControl)
	case errors.Is(err, domain.ErrAdminControlUnavailable):
		return status.Error(codes.Unavailable, domain.MessageAdminControlUnavailable)
	default:
		return rpcError(err)
	}
}

var _ strategyv1.StrategyControlServer = (*ControlServer)(nil)
