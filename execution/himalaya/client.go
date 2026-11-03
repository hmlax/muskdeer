// Package himalaya consumes the exchange's supplied Go SDK without changing it.
package himalaya

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	orderclient "gitlab.com/himalaya/lukla-go"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

const (
	messageInvalidSDKConfig = "himalaya SDK configuration is invalid"
	messageInvalidSDKSubmit = "himalaya SDK submission is invalid"
	messageRepeatedDispatch = "himalaya SDK submission was already dispatched"
	maximumOperationTimeout = 5 * time.Second
)

var (
	// ErrInvalidConfig rejects incomplete transport configuration.
	ErrInvalidConfig = errors.New(messageInvalidSDKConfig)
	// ErrInvalidSubmit rejects malformed orders before the SDK call.
	ErrInvalidSubmit    = errors.New(messageInvalidSDKSubmit)
	errRepeatedDispatch = errors.New(messageRepeatedDispatch)
)

// Config configures one owned SDK connection, created by the execution worker.
type Config struct {
	Target      string
	Token       string
	Timeout     time.Duration
	DialOptions []grpc.DialOption
}

// Admission binds a previously persisted intent to the final Muskdeer sender.
// It is local to Muskdeer and is never added to the exchange's wire protocol.
type Admission struct {
	IntentID int64
	OwnerID  string
	Epoch    uint64
	UserID   int64
	FundID   int64
	ClientID string
	Digest   [32]byte
}

// SubmissionGuard verifies the durable intent, account ownership and epoch at
// the final SDK call. Its callback must receive the supplied context or a child
// bounded by the current lease; it must never retry a dispatched callback.
type SubmissionGuard interface {
	WithSubmission(context.Context, Admission, func(context.Context) error) error
}

// SubmitRequest carries exact decimal values and the already persisted identity.
type SubmitRequest struct {
	IntentID int64
	OwnerID  string
	Epoch    uint64
	UserID   int64
	FundID   int64
	Symbol   string
	ClientID string
	Side     domain.Side
	Price    decimal.Decimal
	Quantity decimal.Decimal
	Leverage uint32
}

// Client owns one upstream SDK connection. A nil guard exposes reads only.
type Client struct {
	sdk     *orderclient.Client
	guard   SubmissionGuard
	timeout time.Duration
}

// NewClient configures the supplied SDK and disables configured write retries.
// Call this from the execution worker lifecycle, not a Wire provider.
func NewClient(ctx context.Context, cfg Config, guard SubmissionGuard) (*Client, error) {
	if ctx == nil || strings.TrimSpace(cfg.Target) == "" || cfg.Token == "" || cfg.Timeout <= 0 || cfg.Timeout > maximumOperationTimeout {
		return nil, ErrInvalidConfig
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options := append([]grpc.DialOption{}, cfg.DialOptions...)
	options = append(options, grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(), grpc.WithNoProxy(),
		grpc.WithChainUnaryInterceptor(invokeSDKOnce))
	sdk, err := orderclient.New(ctx, cfg.Target,
		orderclient.WithAuthToken(cfg.Token),
		orderclient.WithTimeout(cfg.Timeout),
		orderclient.WithDialOptions(options...),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		sdk:     sdk,
		guard:   guard,
		timeout: cfg.Timeout,
	}, nil
}

// Close releases the SDK connection without changing exchange orders/positions.
func (c *Client) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidConfig
	}
	if c == nil || c.sdk == nil {
		return nil
	}
	return c.sdk.Close()
}

// Submit invokes the existing SDK Place once. An ambiguous result retains the
// original client identity and must be queried, never blindly resubmitted.
func (c *Client) Submit(ctx context.Context, request SubmitRequest) (*pb.PlaceOrderResponse, error) {
	if c == nil || c.sdk == nil || ctx == nil {
		return nil, ErrInvalidSubmit
	}
	if c.guard == nil {
		return nil, domain.ErrLeaseLost
	}
	input, err := sdkInput(request)
	if err != nil {
		return nil, err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return nil, err
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	admission := Admission{
		IntentID: request.IntentID,
		OwnerID:  request.OwnerID,
		Epoch:    request.Epoch,
		UserID:   request.UserID,
		FundID:   request.FundID,
		ClientID: request.ClientID,
		Digest:   sha256.Sum256(encoded),
	}
	var dispatched atomic.Bool
	var response *pb.PlaceOrderResponse
	err = c.guard.WithSubmission(op, admission, func(send context.Context) error {
		if send == nil {
			return ErrInvalidSubmit
		}
		if err := op.Err(); err != nil {
			return err
		}
		if err := send.Err(); err != nil {
			return err
		}
		deadline, bounded := send.Deadline()
		operationDeadline, _ := op.Deadline()
		if !bounded || deadline.After(operationDeadline) {
			return ErrInvalidSubmit
		}
		if !dispatched.CompareAndSwap(false, true) {
			return errRepeatedDispatch
		}
		var callErr error
		response, callErr = c.sdk.Place(send, input)
		return callErr
	})
	if err != nil {
		if dispatched.Load() {
			return response, &outcomeFailure{cause: err}
		}
		return nil, err
	}
	if !dispatched.Load() || response.GetOrderId() == 0 || response.GetClientId() != request.ClientID {
		return response, &outcomeFailure{cause: ErrInvalidSubmit}
	}
	return response, nil
}

type outcomeFailure struct{ cause error }

func (*outcomeFailure) Error() string   { return domain.MessageOutcomeUnknown }
func (e *outcomeFailure) Unwrap() error { return errors.Join(domain.ErrOutcomeUnknown, e.cause) }

func sdkInput(request SubmitRequest) (*pb.PlaceRequest, error) {
	if request.IntentID <= 0 || request.Epoch == 0 || !validIdentity(request.OwnerID) || request.UserID <= 0 || request.FundID <= 0 ||
		!validSymbol(request.Symbol) || !validIdentity(request.ClientID) || request.Leverage == 0 || request.Leverage > 150 {
		return nil, ErrInvalidSubmit
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	if _, err := domain.ParseDecimal(request.Price.String(), rule); err != nil {
		return nil, errors.Join(ErrInvalidSubmit, err)
	}
	if _, err := domain.ParseDecimal(request.Quantity.String(), rule); err != nil {
		return nil, errors.Join(ErrInvalidSubmit, err)
	}
	var side pb.Side
	switch request.Side {
	case domain.SideBuy:
		side = pb.Side_SIDE_BUY
	case domain.SideSell:
		side = pb.Side_SIDE_SELL
	default:
		return nil, ErrInvalidSubmit
	}
	return &pb.PlaceRequest{
		UserId:       request.UserID,
		FundId:       request.FundID,
		Symbol:       request.Symbol,
		Side:         side,
		OrderKind:    pb.OrderKind_ORDER_KIND_POST_ONLY,
		Price:        request.Price.String(),
		Quantity:     request.Quantity.String(),
		TimeInForce:  pb.TimeInForce_TIME_IN_FORCE_GTC,
		Leverage:     int32(request.Leverage),
		PositionType: pb.PositionType_POSITION_TYPE_CROSS,
		ClientId:     request.ClientID,
	}, nil
}

func validSymbol(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, char := range value {
		upper := char >= 'A' && char <= 'Z'
		digit := char >= '0' && char <= '9'
		if !upper && !digit {
			return false
		}
	}
	return true
}

func validIdentity(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		letter := char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
		digit := char >= '0' && char <= '9'
		if !letter && !digit && !strings.ContainsRune("._-:", char) {
			return false
		}
	}
	return true
}
