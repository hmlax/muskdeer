package executiongrpc

import (
	"context"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/orderwire"
)

type recoveryClient struct{ client executionv1.ExecutionClient }

// NewRecovery borrows the owned unary factory. Each observation resolves fresh
// discovery and uses the factory's no-replay transport; construction does no I/O.
func NewRecovery(factory *rpcclient.Factory) (domain.IntentRecovery, error) {
	if factory == nil {
		return nil, domain.ErrInvalidOrder
	}
	connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return nil, err
	}
	return &recoveryClient{client: executionv1.NewExecutionClient(connection)}, nil
}

func (c *recoveryClient) Recover(ctx context.Context, accountID string, intentID int64) (domain.IntentSnapshot, error) {
	if !identifier(accountID) || intentID <= 0 {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	reply, err := c.client.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: accountID,
		Lookup:    &executionv1.FindOrderRequest_IntentId{IntentId: intentID},
	})
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	if reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || reply.Order == nil || reply.Order.AccountId != accountID || reply.Order.IntentId != intentID {
		return domain.IntentSnapshot{}, domain.ErrInvalidOrder
	}
	return orderwire.Snapshot(reply.Order)
}
