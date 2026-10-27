// Package executiongrpc delivers typed OMS operations over authenticated gRPC.
package executiongrpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/orderwire"
)

// Stable public errors and pagination defaults.
const (
	MessageInvalidRequest = "invalid execution request"
	MessageDenied         = "service is not allowed for this execution method"
	MessageUnavailable    = "execution operation failed"
	MessageNotFound       = "execution resource was not found"
	defaultPageSize       = 20
)

// Authority proposes the process-owned execution lease for a specific account.
type Authority interface {
	ForAccount(ctx context.Context, accountID string) (domain.LeaseToken, error)
}

// Dependencies are borrowed; callers own their database/gateway lifecycle.
type Dependencies struct {
	Intents       domain.IntentService
	IntentStore   domain.IntentRepository
	Cancellations domain.CancellationService
	Queries       domain.ExecutionQueries
	Authority     Authority
}

// Server implements every declared execution RPC; no method is a placeholder.
type Server struct {
	executionv1.UnimplementedExecutionServer
	deps Dependencies
}

var _ executionv1.ExecutionServer = (*Server)(nil)

// New registers no global service and starts no task.
func New(deps Dependencies) (*Server, error) {
	if deps.Intents == nil || deps.IntentStore == nil || deps.Cancellations == nil || deps.Queries == nil || deps.Authority == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &Server{deps: deps}, nil
}

// SubmitIntent durably accepts and dispatches one caller-owned trading intent.
func (s *Server) SubmitIntent(ctx context.Context, request *executionv1.SubmitIntentRequest) (*executionv1.OrderReply, error) {
	if err := authorized(ctx, true); err != nil {
		return nil, err
	}
	input, err := orderwire.Submit(ctx, request, time.Now())
	if err != nil {
		return nil, rpcError(err)
	}
	owner, err := s.deps.Authority.ForAccount(ctx, input.AccountID)
	if err != nil {
		return nil, rpcError(err)
	}
	order, err := s.deps.Intents.Submit(ctx, input, owner)
	if err != nil {
		return nil, rpcError(err)
	}
	return orderReply(order)
}

// CancelOrder retains independent cancellation-request and core order outcomes.
func (s *Server) CancelOrder(ctx context.Context, request *executionv1.CancelOrderRequest) (*executionv1.OrderReply, error) {
	if err := authorized(ctx, true); err != nil {
		return nil, err
	}
	input, err := orderwire.Cancel(ctx, request, time.Now())
	if err != nil {
		return nil, rpcError(err)
	}
	owner, err := s.deps.Authority.ForAccount(ctx, input.AccountID)
	if err != nil {
		return nil, rpcError(err)
	}
	result, err := s.deps.Cancellations.Cancel(ctx, input, owner)
	if err != nil {
		return nil, rpcError(err)
	}
	return cancellationReply(result)
}

// FindOrder queries the original core identity to reconcile an existing intent.
func (s *Server) FindOrder(ctx context.Context, request *executionv1.FindOrderRequest) (*executionv1.OrderReply, error) {
	if err := authorized(ctx, false); err != nil {
		return nil, err
	}
	if !validRequest(request) || !identifier(request.AccountId) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	var (
		order domain.IntentSnapshot
		err   error
	)
	switch lookup := request.Lookup.(type) {
	case *executionv1.FindOrderRequest_IntentId:
		if lookup == nil || lookup.IntentId <= 0 {
			return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
		}
		order, err = s.deps.Intents.Recover(ctx, request.AccountId, lookup.IntentId)
	case *executionv1.FindOrderRequest_ClientOrderId:
		if lookup == nil || !identifier(lookup.ClientOrderId) {
			return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
		}
		order, err = s.deps.IntentStore.ByClient(ctx, request.AccountId, lookup.ClientOrderId)
		if err == nil {
			order, err = s.deps.Intents.Recover(ctx, request.AccountId, order.ID)
		}
	default:
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	if err != nil {
		return nil, rpcError(err)
	}
	return orderReply(order)
}

// FindCancellation observes a durable request without submitting another cancel.
func (s *Server) FindCancellation(ctx context.Context, request *executionv1.FindCancellationRequest) (*executionv1.OrderReply, error) {
	if err := authorized(ctx, false); err != nil {
		return nil, err
	}
	if !validRequest(request) || !identifier(request.AccountId) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	var (
		result domain.CancellationResult
		err    error
	)
	switch {
	case request.CancellationId > 0 && request.IdempotencyKey == "":
		result, err = s.deps.Cancellations.Recover(ctx, request.AccountId, request.CancellationId)
	case request.CancellationId == 0 && boundedIdentifier(request.IdempotencyKey, 128):
		result, err = s.deps.Cancellations.RecoverByKey(ctx, request.AccountId, request.IdempotencyKey)
	default:
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	if err != nil {
		return nil, rpcError(err)
	}
	return cancellationReply(result)
}

// ListOrders returns a bounded keyset page of persisted OMS observations.
func (s *Server) ListOrders(ctx context.Context, request *executionv1.ListOrdersRequest) (*executionv1.ListOrdersReply, error) {
	if err := authorized(ctx, false); err != nil {
		return nil, err
	}
	if !validRequest(request) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	filter, err := queryFilter(request.AccountId, request.StrategyInstanceId, request.InstrumentId, request.AfterId, request.PageSize)
	if err != nil {
		return nil, err
	}
	filter.OpenOnly = request.OpenOnly
	orders, next, err := s.deps.Queries.Orders(ctx, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	result := &executionv1.ListOrdersReply{
		Orders:      make([]*executionv1.OrderView, 0, len(orders)),
		NextAfterId: next,
	}
	for _, order := range orders {
		view, err := orderwire.Order(order)
		if err != nil {
			return nil, status.Error(codes.Internal, MessageUnavailable)
		}
		result.Orders = append(result.Orders, view)
	}
	return result, nil
}

// ListFills returns scoped immutable source fills with exact decimal strings.
func (s *Server) ListFills(ctx context.Context, request *executionv1.ListFillsRequest) (*executionv1.ListFillsReply, error) {
	if err := authorized(ctx, false); err != nil {
		return nil, err
	}
	if !validRequest(request) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	filter, err := queryFilter(request.AccountId, request.StrategyInstanceId, request.InstrumentId, request.AfterId, request.PageSize)
	if err != nil {
		return nil, err
	}
	fills, next, err := s.deps.Queries.Fills(ctx, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	result := &executionv1.ListFillsReply{
		Fills:       make([]*executionv1.FillView, 0, len(fills)),
		NextAfterId: next,
	}
	for _, fill := range fills {
		view, err := orderwire.Fill(fill)
		if err != nil {
			return nil, status.Error(codes.Internal, MessageUnavailable)
		}
		result.Fills = append(result.Fills, view)
	}
	return result, nil
}

// GetAccount returns balances read under one authoritative account revision.
func (s *Server) GetAccount(ctx context.Context, request *executionv1.GetAccountRequest) (*executionv1.AccountReply, error) {
	if err := authorized(ctx, false); err != nil {
		return nil, err
	}
	if !validRequest(request) || !identifier(request.AccountId) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	account, err := s.deps.Queries.Account(ctx, request.AccountId)
	if err != nil {
		return nil, rpcError(err)
	}
	result, err := orderwire.Account(account)
	if err != nil {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	return result, nil
}

func authorized(ctx context.Context, write bool) error {
	writer := rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) || rpcsecurity.HasService(ctx, rpcsecurity.ServiceCron)
	reader := rpcsecurity.HasService(ctx, rpcsecurity.ServiceAPI) || rpcsecurity.HasService(ctx, rpcsecurity.ServicePortfolio)
	if !writer && (write || !reader) {
		return status.Error(codes.PermissionDenied, MessageDenied)
	}
	if err := ctx.Err(); err != nil {
		return rpcError(err)
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > domain.MaxIntentLifetime {
		return status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	return nil
}

func validRequest(message proto.Message) bool {
	return message != nil && message.ProtoReflect().IsValid() && len(message.ProtoReflect().GetUnknown()) == 0 && proto.Size(message) <= 64*1024
}

func identifier(value string) bool {
	return boundedIdentifier(value, 64)
}

func boundedIdentifier(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func queryFilter(account string, strategy, instrument, after int64, limit int32) (domain.ExecutionFilter, error) {
	if !identifier(account) || strategy < 0 || instrument < 0 || after < 0 || limit < 0 || limit > domain.MaximumExecutionPage {
		return domain.ExecutionFilter{}, status.Error(codes.InvalidArgument, MessageInvalidRequest)
	}
	if limit == 0 {
		limit = defaultPageSize
	}
	return domain.ExecutionFilter{
		AccountID:    account,
		StrategyID:   strategy,
		InstrumentID: instrument,
		AfterID:      after,
		Limit:        int(limit),
	}, nil
}

func orderReply(order domain.IntentSnapshot) (*executionv1.OrderReply, error) {
	view, err := orderwire.Order(order)
	if err != nil {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	return &executionv1.OrderReply{Order: view}, nil
}

func cancellationReply(result domain.CancellationResult) (*executionv1.OrderReply, error) {
	reply, err := orderReply(result.Order)
	if err != nil {
		return nil, err
	}
	reply.Cancellation, err = orderwire.Cancellation(result.Request)
	if err != nil {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	return reply, nil
}

func rpcError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	case errors.Is(err, domain.ErrInvalidOrder), errors.Is(err, domain.ErrUnsupportedOrder):
		return status.Error(codes.InvalidArgument, MessageInvalidRequest)
	case errors.Is(err, domain.ErrCoreNotFound):
		return status.Error(codes.NotFound, MessageNotFound)
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, domain.MessageIdempotencyConflict)
	case errors.Is(err, domain.ErrLeaseHeld), errors.Is(err, domain.ErrLeaseLost):
		return status.Error(codes.FailedPrecondition, domain.MessageLeaseLost)
	case errors.Is(err, domain.ErrRiskDenied):
		return status.Error(codes.FailedPrecondition, domain.MessageRiskDenied)
	case errors.Is(err, domain.ErrInsufficientFunds):
		return status.Error(codes.FailedPrecondition, domain.MessageInsufficientFunds)
	case errors.Is(err, domain.ErrSelfTrade):
		return status.Error(codes.FailedPrecondition, domain.MessageSelfTrade)
	default:
		return status.Error(codes.Unavailable, MessageUnavailable)
	}
}
