package himalaya

import (
	"context"
	"crypto/sha256"
	"errors"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// Config is connection setup passed to the injected gateway.
type Config = gateway.Config

// Admission identifies one durable order and its execution owner.
type Admission = gateway.Admission

// SubmissionGuard authorizes the final send using business state.
type SubmissionGuard = gateway.SubmissionGuard

// SubmitRequest is the venue-neutral order command.
type SubmitRequest = gateway.SubmitRequest

// Scope identifies an existing external fund and symbol.
type Scope = gateway.Scope

// CancelRequest targets one immutable external order ID.
type CancelRequest = gateway.CancelRequest

// CancelAdmission identifies one durable cancellation attempt.
type CancelAdmission = gateway.CancelAdmission

// CancellationGuard retains execution authority through cancellation.
type CancellationGuard = gateway.CancellationGuard

// ErrInvalidConfig, ErrInvalidSubmit and ErrInvalidSnapshot share gateway error identities.
var (
	ErrInvalidConfig    = gateway.ErrInvalidConfig
	ErrInvalidSubmit    = gateway.ErrInvalidSubmit
	ErrInvalidSnapshot  = gateway.ErrInvalidSnapshot
	errSDKNotDispatched = gateway.ErrNotDispatched
)

const maximumOperationTimeout = gateway.MaximumOperationTimeout

const messageRepeatedDispatch = "himalaya SDK submission was already dispatched"

var errRepeatedDispatch = errors.New(messageRepeatedDispatch)

// Factory constructs the SDK adapter and preserves its request wire digests.
type Factory struct{}

// Dial creates one owned SDK connection for the execution lifecycle.
func (Factory) Dial(ctx context.Context, cfg Config, guard SubmissionGuard) (gateway.Client, error) {
	return NewClient(ctx, cfg, guard)
}

// RequestDigest preserves the exact persisted SDK request identity.
func (Factory) RequestDigest(request SubmitRequest) ([32]byte, error) {
	input, err := sdkInput(request)
	if err != nil {
		return [32]byte{}, err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	return sha256.Sum256(encoded), err
}

// CancelDigest preserves the exact persisted cancellation identity.
func (Factory) CancelDigest(request CancelRequest) ([32]byte, error) {
	_, digest, err := cancelInput(request)
	return digest, err
}

func submitResult(response *pb.PlaceOrderResponse) *gateway.SubmitResult {
	if response == nil {
		return nil
	}
	return &gateway.SubmitResult{
		OrderID:  response.OrderId,
		ClientID: response.ClientId,
	}
}

var _ gateway.Factory = Factory{}
var _ gateway.Client = (*Client)(nil)
