package domain

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
)

// Gateway command identifiers are independent of OMS transport correlation.
const (
	GatewayActionSubmit        = "SUBMIT"
	GatewayActionCancel        = "CANCEL"
	MessageInsufficientFunds   = "insufficient core funds"
	MessageIdempotencyConflict = "idempotency key has different business content"
	MessageCoreNotFound        = "core record not found"
	MessageSelfTrade           = "self-trade prevention rejected the operation"
	MessageUnsupportedOrder    = "order capability is unsupported"
	MessageCoreUnavailable     = "core persistence operation failed"
	MessageOutcomeUnknown      = "trading outcome is unknown; query by stable identity"
)

// Core failures preserve safety without exposing database text.
var (
	ErrInsufficientFunds   = errors.New(MessageInsufficientFunds)
	ErrIdempotencyConflict = errors.New(MessageIdempotencyConflict)
	ErrCoreNotFound        = errors.New(MessageCoreNotFound)
	ErrSelfTrade           = errors.New(MessageSelfTrade)
	ErrUnsupportedOrder    = errors.New(MessageUnsupportedOrder)
	ErrOutcomeUnknown      = errors.New(MessageOutcomeUnknown)
)

// GatewaySubmit binds stable client identity to two independently owned scopes.
type GatewaySubmit struct {
	IntentID           int64
	Input              OrderInput
	ClientOrderID      string
	ExecutionAuthority LeaseToken
}

// GatewayCancel retains the original client order identity across takeover.
type GatewayCancel struct {
	IntentID           int64
	Input              CancelInput
	ExecutionAuthority LeaseToken
}

// CoreOrder is a snapshot of the authoritative simulated core, not OMS state.
type CoreOrder struct {
	ID             int64
	ClientOrderID  string
	AccountID      string
	StrategyID     int64
	ConfigVersion  int64
	InstrumentID   int64
	Side           Side
	Price          decimal.Decimal
	Quantity       decimal.Decimal
	FilledQuantity decimal.Decimal
	TimeInForce    TimeInForce
	ReduceOnly     bool
	State          OrderState
	CancelState    CancelState
	FrozenAsset    string
	FrozenAmount   decimal.Decimal
	QuoteFeeRate   decimal.Decimal
	Revision       int64
}

// CoreBalance contains exact spot funds, with no synthetic negative credit.
type CoreBalance struct {
	Asset     string
	Available decimal.Decimal
	Frozen    decimal.Decimal
}

// CoreFillRequest explicitly drives simulated external liquidity.
type CoreFillRequest struct {
	AccountID           string
	ClientOrderID       string
	TradeID             string
	CounterpartySubject string
	Price               decimal.Decimal
	Quantity            decimal.Decimal
}

// CoreFill records one immutable, naturally deduplicated execution fact.
type CoreFill struct {
	ID            int64
	CoreOrderID   int64
	TradeID       string
	Price         decimal.Decimal
	Quantity      decimal.Decimal
	QuoteNotional decimal.Decimal
	FeeAmount     decimal.Decimal
	FeeAsset      string
}

// TradingGateway is the execution adapter's narrow trading boundary.
type TradingGateway interface {
	Submit(ctx context.Context, command GatewaySubmit) (CoreOrder, error)
	Cancel(ctx context.Context, command GatewayCancel) (CoreOrder, error)
	Find(ctx context.Context, accountID, clientOrderID string) (CoreOrder, error)
	Balances(ctx context.Context, accountID string) ([]CoreBalance, error)
}

// Simulator adds privileged scenario control to the trading contract. Fill and
// ConfirmCancel are never management trade APIs.
type Simulator interface {
	TradingGateway
	Fill(ctx context.Context, request CoreFillRequest) (CoreFill, error)
	ConfirmCancel(ctx context.Context, accountID, clientOrderID string) (CoreOrder, error)
}

// SpotSettlement describes simultaneous debits/credits at one fill boundary.
type SpotSettlement struct {
	Notional       decimal.Decimal
	Fee            decimal.Decimal
	Spent          decimal.Decimal
	Received       decimal.Decimal
	RemainingFunds decimal.Decimal
	Released       decimal.Decimal
}

// FixedDecimal checks already typed values without expanding hostile exponents.
func FixedDecimal(value decimal.Decimal, integerDigits, scale int, allowZero bool) bool {
	if value.IsNegative() || value.IsZero() && !allowZero || value.Exponent() < -int32(scale) || value.Exponent() > int32(integerDigits) {
		return false
	}
	digits := len(value.Coefficient().String())
	return digits <= integerDigits+scale && digits+int(value.Exponent()) <= integerDigits
}

// SpotReserve includes quote fees for buys and the base quantity for sells.
// Fees are charged in quote currency, truncated to 36 fractional digits per
// execution. This explicit simulator policy is not a real venue's fee policy.
func SpotReserve(side Side, price, quantity, feeRate decimal.Decimal) (decimal.Decimal, error) {
	if !FixedDecimal(price, 20, 18, false) || !FixedDecimal(quantity, 20, 18, true) ||
		!FixedDecimal(feeRate, 1, 18, true) || !feeRate.LessThan(decimal.NewFromInt(1)) {
		return decimal.Zero, ErrInvalidOrder
	}
	switch side {
	case SideBuy:
		notional := price.Mul(quantity)
		return notional.Add(notional.Mul(feeRate).Truncate(36)), nil
	case SideSell:
		return quantity, nil
	default:
		return decimal.Zero, ErrInvalidSide
	}
}

// SettleSpotFill conserves the frozen reserve, with price improvement released
// immediately. Partial fills remain valid while cancellation is pending.
func SettleSpotFill(order CoreOrder, price, quantity decimal.Decimal) (SpotSettlement, error) {
	if !FixedDecimal(order.Price, 20, 18, false) || !FixedDecimal(order.Quantity, 20, 18, false) ||
		!FixedDecimal(order.FrozenAmount, 40, 36, true) || !FixedDecimal(price, 20, 18, false) || !FixedDecimal(quantity, 20, 18, false) ||
		!FixedDecimal(order.FilledQuantity, 20, 18, true) || quantity.GreaterThan(order.Quantity.Sub(order.FilledQuantity)) ||
		(order.Side == SideBuy && price.GreaterThan(order.Price)) || (order.Side == SideSell && price.LessThan(order.Price)) {
		return SpotSettlement{}, ErrInvalidOrder
	}
	remaining, err := SpotReserve(order.Side, order.Price, order.Quantity.Sub(order.FilledQuantity).Sub(quantity), order.QuoteFeeRate)
	if err != nil {
		return SpotSettlement{}, err
	}
	notional := price.Mul(quantity)
	fee := notional.Mul(order.QuoteFeeRate).Truncate(36)
	spent, received := quantity, notional.Sub(fee)
	if order.Side == SideBuy {
		spent, received = notional.Add(fee), quantity
	}
	released := order.FrozenAmount.Sub(spent).Sub(remaining)
	if released.IsNegative() {
		return SpotSettlement{}, ErrInsufficientFunds
	}
	return SpotSettlement{
		Notional:       notional,
		Fee:            fee,
		Spent:          spent,
		Received:       received,
		RemainingFunds: remaining,
		Released:       released,
	}, nil
}
