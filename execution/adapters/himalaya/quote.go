package himalaya

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// SubmitQuote sends a maker order once, without a local intent or receipt.
func (c *Client) SubmitQuote(ctx context.Context, request gateway.QuoteRequest) (*gateway.SubmitResult, error) {
	if c == nil || c.sdk == nil || ctx == nil {
		return nil, ErrInvalidSubmit
	}
	input, err := sdkOrderInput(SubmitRequest{
		UserID:   request.Scope.UserID,
		FundID:   request.Scope.FundID,
		Symbol:   request.Scope.Symbol,
		ClientID: request.ClientID,
		Side:     request.Side,
		Price:    request.Price,
		Quantity: request.Quantity,
		Leverage: request.Leverage,
	})
	if err != nil {
		return nil, err
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := op.Err(); err != nil {
		return nil, errors.Join(errSDKNotDispatched, err)
	}
	trace := &sdkDispatchTrace{}
	response, err := c.sdk.Place(context.WithValue(op, sdkDispatchContextKey{}, trace), input)
	if err != nil {
		if trace.entered.Load() && !trace.body.Load() {
			return nil, errors.Join(errSDKNotDispatched, err)
		}
		return submitResult(response), &outcomeFailure{cause: err}
	}
	if response.GetOrderId() == 0 || response.GetClientId() != request.ClientID {
		return submitResult(response), &outcomeFailure{cause: ErrInvalidSubmit}
	}
	return submitResult(response), nil
}

// CancelQuote addresses the venue order ID directly and sends once.
func (c *Client) CancelQuote(ctx context.Context, request gateway.CancelRequest) error {
	if c == nil || c.sdk == nil || ctx == nil {
		return ErrInvalidSubmit
	}
	input, _, err := cancelInput(request)
	if err != nil {
		return err
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := op.Err(); err != nil {
		return err
	}
	response, err := c.sdk.CancelOrder(op, input)
	if err != nil {
		return &outcomeFailure{cause: err}
	}
	if !response.GetSuccess() {
		return &outcomeFailure{cause: ErrInvalidSubmit}
	}
	return nil
}
