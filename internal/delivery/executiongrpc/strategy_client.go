package executiongrpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/orderwire"
)

type strategyClientFactory interface {
	Client(rpcclient.Target) (grpc.ClientConnInterface, error)
}

type strategyClient struct {
	factory strategyClientFactory
	routing domain.ExecutionRouting
	reader  executionv1.ExecutionClient
}

type strategyWriteOutcome struct{ cause error }

func (*strategyWriteOutcome) Error() string { return domain.MessageOutcomeUnknown }
func (e *strategyWriteOutcome) Unwrap() error {
	return errors.Join(domain.ErrOutcomeUnknown, e.cause)
}

// NewStrategyClient borrows Deer-owned transport and current account routing.
// Construction performs no discovery, acquisition or network request. Writes
// select an explicit current owner; read-only recovery remains stateless.
func NewStrategyClient(factory *rpcclient.Factory, routing domain.ExecutionRouting) (domain.StrategyExecution, error) {
	if factory == nil {
		return nil, domain.ErrInvalidOrder
	}
	return newStrategyClient(factory, routing)
}

func newStrategyClient(factory strategyClientFactory, routing domain.ExecutionRouting) (domain.StrategyExecution, error) {
	if factory == nil || routing == nil {
		return nil, domain.ErrInvalidOrder
	}
	connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyClient{
		factory: factory,
		routing: routing,
		reader:  executionv1.NewExecutionClient(connection),
	}, nil
}

func (c *strategyClient) Submit(ctx context.Context, input domain.OrderInput) (domain.IntentSnapshot, error) {
	request, err := strategySubmitRequest(ctx, input)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	writer, err := c.writer(ctx, input.AccountID)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	reply, err := writer.SubmitIntent(ctx, request)
	if err != nil {
		return domain.IntentSnapshot{}, &strategyWriteOutcome{cause: err}
	}
	clientID, err := domain.IntentClientOrderID(input.AccountID, input.IdempotencyKey)
	if err != nil {
		return domain.IntentSnapshot{}, &strategyWriteOutcome{cause: err}
	}
	order, err := strategyOrderObservation(reply, input.AccountID, clientID)
	if err != nil {
		return domain.IntentSnapshot{}, &strategyWriteOutcome{cause: err}
	}
	actual := order.Input
	if reply.Cancellation != nil || actual.Authority.StrategyID != input.Authority.StrategyID || actual.Authority.ConfigVersion != input.Authority.ConfigVersion ||
		actual.InstrumentID != input.InstrumentID || actual.Side != input.Side || actual.TimeInForce != input.TimeInForce || actual.ReduceOnly != input.ReduceOnly ||
		!actual.Price.Equal(input.Price) || !actual.Quantity.Equal(input.Quantity) {
		return domain.IntentSnapshot{}, &strategyWriteOutcome{cause: domain.ErrInvalidOrder}
	}
	return order, nil
}

func (c *strategyClient) Cancel(ctx context.Context, input domain.CancelInput) (domain.CancellationResult, error) {
	request, err := strategyCancelRequest(ctx, input)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	writer, err := c.writer(ctx, input.AccountID)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	reply, err := writer.CancelOrder(ctx, request)
	if err != nil {
		return domain.CancellationResult{}, &strategyWriteOutcome{cause: err}
	}
	result, err := strategyCancellationObservation(reply, input.AccountID, input.IdempotencyKey)
	if err != nil {
		return domain.CancellationResult{}, &strategyWriteOutcome{cause: err}
	}
	if result.Order.ClientOrderID != input.ClientOrderID || result.Order.Input.Authority.StrategyID != input.Authority.StrategyID || result.Order.Input.InstrumentID != input.InstrumentID {
		return domain.CancellationResult{}, &strategyWriteOutcome{cause: domain.ErrInvalidOrder}
	}
	return result, nil
}

func (c *strategyClient) FindOrder(ctx context.Context, accountID, idempotencyKey string) (domain.IntentSnapshot, error) {
	if err := strategyQueryContext(ctx, accountID); err != nil {
		return domain.IntentSnapshot{}, err
	}
	clientID, err := domain.IntentClientOrderID(accountID, idempotencyKey)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	return c.Order(ctx, accountID, clientID)
}

func (c *strategyClient) Order(ctx context.Context, accountID, clientID string) (domain.IntentSnapshot, error) {
	if err := strategyQueryContext(ctx, accountID); err != nil {
		return domain.IntentSnapshot{}, err
	}
	if !boundedIdentifier(clientID, 64) {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	reply, err := c.reader.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: accountID,
		Lookup:    &executionv1.FindOrderRequest_ClientOrderId{ClientOrderId: clientID},
	})
	if err != nil {
		return domain.IntentSnapshot{}, strategyObservationFailure(err)
	}
	order, err := strategyOrderObservation(reply, accountID, clientID)
	if err != nil || reply.Cancellation != nil {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return order, nil
}

func (c *strategyClient) FindCancellation(ctx context.Context, accountID, idempotencyKey string) (domain.CancellationResult, error) {
	if err := strategyQueryContext(ctx, accountID); err != nil {
		return domain.CancellationResult{}, err
	}
	if !boundedIdentifier(idempotencyKey, 128) {
		return domain.CancellationResult{}, domain.ErrInvalidOrder
	}
	reply, err := c.reader.FindCancellation(ctx, &executionv1.FindCancellationRequest{
		AccountId:      accountID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return domain.CancellationResult{}, strategyObservationFailure(err)
	}
	return strategyCancellationObservation(reply, accountID, idempotencyKey)
}

func strategyObservationFailure(err error) error {
	if status.Code(err) == codes.NotFound {
		return domain.ErrCoreNotFound
	}
	return &strategyReadOutcome{cause: err}
}

type strategyReadOutcome struct{ cause error }

func (*strategyReadOutcome) Error() string { return domain.MessageOutcomeUnknown }
func (e *strategyReadOutcome) Unwrap() error {
	return errors.Join(domain.ErrOutcomeUnknown, e.cause)
}

func (c *strategyClient) writer(ctx context.Context, accountID string) (executionv1.ExecutionClient, error) {
	instance, err := c.routing.ExecutionInstance(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Empty InstanceID means stateless selection in Deer. Never let a missing
	// account owner silently become a write to another healthy provider.
	if !identifier(instance) {
		return nil, domain.ErrLeaseLost
	}
	connection, err := c.factory.Client(rpcclient.Target{
		Service:    rpcsecurity.ServiceExecution,
		InstanceID: instance,
	})
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, domain.ErrInvalidOrder
	}
	return executionv1.NewExecutionClient(connection), nil
}

func strategyQueryContext(ctx context.Context, accountID string) error {
	if ctx == nil {
		return domain.ErrInvalidOrder
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > domain.MaxIntentLifetime || !identifier(accountID) {
		return domain.ErrInvalidOrder
	}
	return nil
}

func strategyOrderObservation(reply *executionv1.OrderReply, accountID, clientID string) (domain.IntentSnapshot, error) {
	if !validRequest(reply) || reply.Order == nil || reply.Order.AccountId != accountID || clientID != "" && reply.Order.ClientOrderId != clientID {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return orderwire.Snapshot(reply.Order)
}

func strategyCancellationObservation(reply *executionv1.OrderReply, accountID, key string) (domain.CancellationResult, error) {
	order, err := strategyOrderObservation(reply, accountID, "")
	if err != nil {
		return domain.CancellationResult{}, err
	}
	view := reply.Cancellation
	if view == nil || len(view.ProtoReflect().GetUnknown()) != 0 || view.Id <= 0 || view.IntentId != order.ID || view.Revision <= 0 || view.IdempotencyKey != key {
		return domain.CancellationResult{}, domain.ErrInvalidOrder
	}
	state, err := strategyCancellationState(view.State)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	return domain.CancellationResult{
		Order: order,
		Request: domain.CancellationSnapshot{
			ID:       view.Id,
			IntentID: view.IntentId,
			State:    state,
			Revision: view.Revision,
			Input: domain.CancelInput{
				AccountID:      accountID,
				IdempotencyKey: key,
				ClientOrderID:  order.ClientOrderID,
				InstrumentID:   order.Input.InstrumentID,
			},
			ErrorCode: view.ErrorCode,
		},
	}, nil
}
