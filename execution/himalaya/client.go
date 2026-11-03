// Package himalaya adapts the development perpetual venue's typed gateway.
package himalaya

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

const (
	messageUnsupportedGateway        = "development gateway trading guarantees are unavailable"
	messageInvalidRequest            = "development gateway request is invalid"
	contractVersion           uint32 = 1
	capabilitiesTimeout              = 2 * time.Second
	maximumOperationTimeout          = 5 * time.Second
)

var (
	// ErrUnsupportedGateway rejects venues without the required trading contract.
	ErrUnsupportedGateway = errors.New(messageUnsupportedGateway)
	// ErrInvalidRequest rejects malformed input before dispatch.
	ErrInvalidRequest = errors.New(messageInvalidRequest)
)

// Client borrows its connection and never owns a second transport pool.
type Client struct{ rpc himalayav1.GatewayClient }

// NewClient checks the venue's declared trading contract before exposing writes.
func NewClient(ctx context.Context, connection grpc.ClientConnInterface) (*Client, error) {
	if ctx == nil || connection == nil {
		return nil, ErrInvalidRequest
	}
	client := &Client{rpc: himalayav1.NewGatewayClient(connection)}
	probe, cancel := context.WithTimeout(ctx, capabilitiesTimeout)
	defer cancel()
	capabilities, err := client.rpc.GetCapabilities(probe, &himalayav1.CapabilitiesRequest{}, grpc.WaitForReady(false))
	if err != nil {
		return nil, errors.Join(ErrUnsupportedGateway, err)
	}
	if !supported(capabilities) {
		return nil, ErrUnsupportedGateway
	}
	return client, nil
}

// Submit sends one durable caller-owned identity without automatic retries.
func (c *Client) Submit(ctx context.Context, request *himalayav1.SubmitRequest) (*himalayav1.SubmitResponse, error) {
	if c == nil || c.rpc == nil || validateContext(ctx) != nil || validateSubmit(request) != nil {
		return nil, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := c.rpc.Submit(ctx, proto.CloneOf(request), grpc.WaitForReady(false), grpc.MaxRetryRPCBufferSize(0))
	if err != nil {
		return nil, &outcomeFailure{cause: err}
	}
	if response.GetOrder().GetId() == 0 || response.Order.ClientId != request.ClientId || response.Order.UserId != request.UserId ||
		response.Order.FundId != request.FundId || response.Order.Symbol != request.Symbol || response.Order.State == himalayav1.OrderState_ORDER_STATE_UNSPECIFIED {
		return nil, &outcomeFailure{cause: ErrInvalidRequest}
	}
	return response, nil
}

type outcomeFailure struct{ cause error }

func (*outcomeFailure) Error() string   { return domain.MessageOutcomeUnknown }
func (e *outcomeFailure) Unwrap() error { return errors.Join(domain.ErrOutcomeUnknown, e.cause) }

func supported(c *himalayav1.Capabilities) bool {
	return c != nil && c.ContractVersion == contractVersion && c.DevelopmentTestFunds && c.DurableClientIdempotency &&
		c.MatchingOwnerFencing && c.QueryByClientId && c.PostOnly && c.OneWayLinearPerpetual && c.OwnerExpiryCancel && validIdentity(c.Incarnation)
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidRequest
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > maximumOperationTimeout {
		return ErrInvalidRequest
	}
	return nil
}

func validateSubmit(r *himalayav1.SubmitRequest) error {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || r.UserId <= 0 || r.FundId <= 0 || !validSymbol(r.Symbol) || !validIdentity(r.ClientId) ||
		r.Side != himalayav1.Side_SIDE_BUY && r.Side != himalayav1.Side_SIDE_SELL || r.PositionMode != himalayav1.PositionMode_POSITION_MODE_ONE_WAY ||
		!r.PostOnly || r.Leverage == 0 || r.Leverage > 150 || r.MaximumQueueAgeMs == 0 || r.MaximumQueueAgeMs > 2_000 {
		return ErrInvalidRequest
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	if _, err := domain.ParseDecimal(r.Price, rule); err != nil {
		return errors.Join(ErrInvalidRequest, err)
	}
	if _, err := domain.ParseDecimal(r.Quantity, rule); err != nil {
		return errors.Join(ErrInvalidRequest, err)
	}
	if !validProof(r.Execution, himalayav1.LeaseKind_LEASE_KIND_EXECUTION, r.UserId, r.FundId, "") ||
		!validProof(r.Strategy, himalayav1.LeaseKind_LEASE_KIND_STRATEGY, r.UserId, r.FundId, r.Symbol) {
		return ErrInvalidRequest
	}
	return nil
}

func validProof(p *himalayav1.Proof, kind himalayav1.LeaseKind, user, fund int64, symbol string) bool {
	return p != nil && p.Scope != nil && p.Epoch > 0 && validIdentity(p.Owner) && p.Scope.Kind == kind &&
		p.Scope.UserId == user && p.Scope.FundId == fund && p.Scope.Symbol == symbol &&
		len(p.ProtoReflect().GetUnknown()) == 0 && len(p.Scope.ProtoReflect().GetUnknown()) == 0
}

func validIdentity(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		asciiLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		asciiDigit := c >= '0' && c <= '9'
		if !asciiLetter && !asciiDigit && !strings.ContainsRune("._-:", c) {
			return false
		}
	}
	return true
}

func validSymbol(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, c := range value {
		asciiUpper := c >= 'A' && c <= 'Z'
		asciiDigit := c >= '0' && c <= '9'
		if !asciiUpper && !asciiDigit {
			return false
		}
	}
	return true
}
