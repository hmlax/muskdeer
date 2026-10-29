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

// RiskQueryServer exposes only current-viewer snapshots to the verified API peer.
type RiskQueryServer struct {
	strategyv1.UnimplementedStrategyRiskQueryServer
	risks domain.AdminStrategyRiskReader
}

// RiskControlServer is registered only with explicit management write opt-in.
type RiskControlServer struct {
	strategyv1.UnimplementedStrategyRiskControlServer
	risks domain.AdminStrategyRiskWriter
}

// NewRiskQuery borrows reads without granting mutation or owning resources.
func NewRiskQuery(risks domain.AdminStrategyRiskReader) (*RiskQueryServer, error) {
	if risks == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &RiskQueryServer{risks: risks}, nil
}

// NewRiskControl borrows audited writes; authorization stays in their transaction.
func NewRiskControl(risks domain.AdminStrategyRiskWriter) (*RiskControlServer, error) {
	if risks == nil {
		return nil, domain.ErrInvalidAdminRisk
	}
	return &RiskControlServer{risks: risks}, nil
}

// GetStrategyRisk reads one exact strategy without provisioning risk capacity.
func (s *RiskQueryServer) GetStrategyRisk(ctx context.Context, request *strategyv1.GetStrategyRiskRequest) (*strategyv1.StrategyRiskState, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.StrategyId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	state, err := s.risks.State(ctx, request.Session.Token, request.StrategyId)
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.StrategyRiskState{
		StrategyId:             state.StrategyID,
		Enabled:                state.Enabled,
		Revision:               state.Revision,
		QuoteAsset:             state.QuoteAsset,
		LimitNotional:          state.LimitNotional.String(),
		ReservedNotional:       state.ReservedNotional.String(),
		State:                  string(state.State),
		DesiredState:           string(state.DesiredState),
		RequestedConfigVersion: state.RequestedConfigVersion,
		MarketDataSource:       string(state.Source),
		UpdatedAtMs:            state.UpdatedAt.UnixMilli(),
	}
	if !strategywire.ValidRiskState(reply, request.StrategyId) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// ChangeStrategyRisk returns a historical flag receipt, never a trading grant.
func (s *RiskControlServer) ChangeStrategyRisk(ctx context.Context, request *strategyv1.ChangeStrategyRiskRequest) (*strategyv1.StrategyRiskReceipt, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, riskRPCError(err)
	}
	if !controlCSRFValid(request.CsrfToken) {
		return nil, riskRPCError(domain.ErrForbidden)
	}
	command, err := strategywire.RiskCommand(request.Input, request.StrategyId, request.IdempotencyKey)
	if err != nil {
		return nil, riskRPCError(err)
	}
	receipt, err := s.risks.Change(ctx, request.Session.Token, request.CsrfToken, command)
	if err != nil {
		return nil, riskRPCError(err)
	}
	reply := riskReceiptProto(receipt)
	if !strategywire.ValidRiskReceipt(reply, command) {
		return nil, riskRPCError(domain.ErrAdminControlUnavailable)
	}
	return reply, nil
}

func riskReceiptProto(value domain.AdminRiskReceipt) *strategyv1.StrategyRiskReceipt {
	return &strategyv1.StrategyRiskReceipt{
		Id:                    value.ID,
		ActorUserId:           value.ActorUserID,
		StrategyId:            value.StrategyID,
		Action:                string(value.Action),
		PreviousEnabled:       value.PreviousEnabled,
		Enabled:               value.Enabled,
		PreviousRevision:      value.PreviousRevision,
		Revision:              value.Revision,
		ExpectedConfigVersion: value.ExpectedConfigVersion,
		ExpectedDesiredState:  string(value.ExpectedDesiredState),
		Reason:                value.Reason,
		CreatedAtMs:           value.CreatedAt.UnixMilli(),
	}
}

func riskRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidAdminRisk):
		return status.Error(codes.InvalidArgument, domain.MessageInvalidAdminRisk)
	case errors.Is(err, domain.ErrAdminRiskRecoveryDenied):
		return status.Error(codes.FailedPrecondition, domain.MessageAdminRiskRecoveryDenied)
	default:
		return controlRPCError(err)
	}
}

var (
	_ strategyv1.StrategyRiskQueryServer   = (*RiskQueryServer)(nil)
	_ strategyv1.StrategyRiskControlServer = (*RiskControlServer)(nil)
)
