package himalaya

import (
	"context"
	"crypto/sha256"
	"sync/atomic"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func cancelInput(request CancelRequest) (*pb.CancelOrderRequest, [32]byte, error) {
	if request.Scope.UserID <= 0 || request.Scope.FundID <= 0 || request.OrderID == 0 || !validSymbol(request.Scope.Symbol) {
		return nil, [32]byte{}, ErrInvalidSubmit
	}
	input := &pb.CancelOrderRequest{
		UserId:  request.Scope.UserID,
		FundId:  request.Scope.FundID,
		Symbol:  request.Scope.Symbol,
		OrderId: request.OrderID,
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	return input, sha256.Sum256(encoded), err
}

// Cancel sends once after durable admission. Success is an acknowledgement only.
func (c *Client) Cancel(ctx context.Context, admission CancelAdmission) error {
	if c == nil || c.sdk == nil || ctx == nil {
		return ErrInvalidSubmit
	}
	guard, ok := c.guard.(CancellationGuard)
	if !ok {
		return domain.ErrLeaseLost
	}
	input, digest, err := cancelInput(admission.Request)
	if err != nil || digest != admission.Digest {
		return ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var dispatched atomic.Bool
	err = guard.WithCancellation(op, admission, func(send context.Context) error {
		if send == nil || send.Err() != nil || op.Err() != nil {
			return ErrInvalidSubmit
		}
		deadline, bounded := send.Deadline()
		outer, _ := op.Deadline()
		if !bounded || deadline.After(outer) || !dispatched.CompareAndSwap(false, true) {
			return ErrInvalidSubmit
		}
		response, err := c.sdk.CancelOrder(send, input)
		if err != nil {
			return err
		}
		if !response.GetSuccess() {
			return domain.ErrOutcomeUnknown
		}
		return nil
	})
	if err != nil && dispatched.Load() {
		return &outcomeFailure{cause: err}
	}
	return err
}
