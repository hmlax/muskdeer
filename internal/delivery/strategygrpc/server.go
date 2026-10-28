// Package strategygrpc serves authenticated, typed management observations.
package strategygrpc

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Fixed peer-denial message and native call budget.
const (
	MessageDenied = "service is not allowed to administer strategies"
	MaximumCall   = 5 * time.Second
)

// Server borrows read-only business capabilities; it owns no background work.
type Server struct {
	strategyv1.UnimplementedStrategyManagementServer
	queries domain.AdminStrategies
}

// New performs no I/O, migration, worker startup or account provisioning.
func New(queries domain.AdminStrategies) (*Server, error) {
	if queries == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &Server{queries: queries}, nil
}

// ListStrategies observes bounded instances with persistent viewer authority.
func (s *Server) ListStrategies(ctx context.Context, request *strategyv1.ListStrategiesRequest) (*strategyv1.ListStrategiesReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.queries.List(ctx, request.Session.Token, domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.ListStrategiesReply{
		Strategies:  make([]*strategyv1.Strategy, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, value := range page.Items {
		reply.Strategies = append(reply.Strategies, strategyProto(value))
	}
	return reply, nil
}

// GetStrategy keeps requested and active versions explicitly separate.
func (s *Server) GetStrategy(ctx context.Context, request *strategyv1.GetStrategyRequest) (*strategyv1.GetStrategyReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if request.StrategyId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	detail, err := s.queries.Detail(ctx, request.Session.Token, request.StrategyId)
	if err != nil {
		return nil, rpcError(err)
	}
	requested, err := configProto(detail.Requested)
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.GetStrategyReply{
		Strategy:        strategyProto(detail.Strategy),
		RequestedConfig: requested,
	}
	if detail.Active != nil {
		reply.ActiveConfig, err = configProto(*detail.Active)
		if err != nil {
			return nil, rpcError(err)
		}
	}
	return reply, nil
}

// ListConfigVersions never crosses the requested instance's configuration set.
func (s *Server) ListConfigVersions(ctx context.Context, request *strategyv1.ListConfigVersionsRequest) (*strategyv1.ListConfigVersionsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if request.StrategyId <= 0 || !pageValid(request.AfterId, request.Limit) {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.queries.Versions(ctx, request.Session.Token, request.StrategyId, domain.AdminPage{
		AfterID: request.AfterId,
		Limit:   int(request.Limit),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	reply := &strategyv1.ListConfigVersionsReply{
		Versions:    make([]*strategyv1.ConfigVersion, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, value := range page.Items {
		version, err := configProto(value)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Versions = append(reply.Versions, version)
	}
	return reply, nil
}

func boundary(ctx context.Context, request proto.Message) error {
	if ctx == nil || !rpcsecurity.HasService(ctx, rpcsecurity.ServiceAPI) {
		return status.Error(codes.PermissionDenied, MessageDenied)
	}
	if err := ctx.Err(); err != nil {
		return rpcError(err)
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > MaximumCall || request == nil || !request.ProtoReflect().IsValid() || len(request.ProtoReflect().GetUnknown()) != 0 || proto.Size(request) > 2048 {
		return rpcError(domain.ErrInvalidAdminQuery)
	}
	return nil
}

func sessionValid(session *strategyv1.AdminSession) error {
	if session == nil || len(session.ProtoReflect().GetUnknown()) != 0 {
		return domain.ErrUnauthenticated
	}
	if len(session.Token) != 43 {
		return domain.ErrUnauthenticated
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(session.Token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != session.Token {
		return domain.ErrUnauthenticated
	}
	return nil
}

func pageValid(after int64, limit int32) bool {
	return after >= 0 && limit >= 1 && limit <= domain.MaxAdminPageSize
}

func rpcError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	case errors.Is(err, domain.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, domain.MessageUnauthenticated)
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, domain.MessageForbidden)
	case errors.Is(err, domain.ErrInvalidAdminQuery):
		return status.Error(codes.InvalidArgument, domain.MessageInvalidAdminQuery)
	case errors.Is(err, domain.ErrAdminNotFound):
		return status.Error(codes.NotFound, domain.MessageAdminNotFound)
	default:
		return status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
}

var _ strategyv1.StrategyManagementServer = (*Server)(nil)
