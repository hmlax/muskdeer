package domain

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

// Decimal validation messages are stable public business messages.
const (
	MessageInvalidDecimal     = "decimal value does not match the field rule"
	MessageInvalidDecimalRule = "decimal field precision must be explicitly defined"
	MessageInvalidIncrement   = "quantity or price increment must be positive and bounded"
	MessageInvalidSide        = "order side must be BUY or SELL"
)

// Decimal errors are transport-independent; delivery maps them to wire errors.
var (
	ErrInvalidDecimal     = errors.New(MessageInvalidDecimal)
	ErrInvalidDecimalRule = errors.New(MessageInvalidDecimalRule)
	ErrInvalidIncrement   = errors.New(MessageInvalidIncrement)
	ErrInvalidSide        = errors.New(MessageInvalidSide)
)

// DecimalRule declares a field's integer/scale limits and sign/zero semantics.
// Limits are independently 1..38 integer digits and 0..38 fractional digits;
// callers choose asset-specific limits compatible with the persisted column.
type DecimalRule struct {
	IntegerDigits int
	Scale         int
	AllowNegative bool
	AllowZero     bool
}

// Side is an explicit order direction; its zero value cannot submit an order.
type Side string

// Supported order directions.
const (
	SideUnspecified Side = ""
	SideBuy         Side = "BUY"
	SideSell        Side = "SELL"
)

// ParseDecimal accepts plain ASCII decimal strings only. Precision is checked
// before calling the decimal library, without floating point or exponent input.
// Canonical integer syntax rejects leading zeroes; trailing fractional zeroes
// count toward the supplied scale. Negative zero still requires both policies.
func ParseDecimal(input string, rule DecimalRule) (decimal.Decimal, error) {
	if rule.IntegerDigits < 1 || rule.IntegerDigits > 38 || rule.Scale < 0 || rule.Scale > 38 {
		return decimal.Zero, ErrInvalidDecimalRule
	}
	if !decimalSyntax(input, rule) {
		return decimal.Zero, ErrInvalidDecimal
	}
	value, err := decimal.NewFromString(input)
	if err != nil {
		return decimal.Zero, errors.Join(ErrInvalidDecimal, err)
	}
	if !rule.AllowZero && value.IsZero() {
		return decimal.Zero, ErrInvalidDecimal
	}
	return value, nil
}

// ParseLedgerDecimal reads the exact numeric(76,36) result range used for
// ledger products and projections. It preserves the smaller ParseDecimal
// contract for individual price/quantity inputs.
func ParseLedgerDecimal(input string, allowNegative, allowZero bool) (decimal.Decimal, error) {
	rule := DecimalRule{
		IntegerDigits: 40,
		Scale:         36,
		AllowNegative: allowNegative,
		AllowZero:     allowZero,
	}
	if !decimalSyntax(input, rule) {
		return decimal.Zero, ErrInvalidDecimal
	}
	value, err := decimal.NewFromString(input)
	if err != nil {
		return decimal.Zero, errors.Join(ErrInvalidDecimal, err)
	}
	if value.IsZero() && !allowZero {
		return decimal.Zero, ErrInvalidDecimal
	}
	return value, nil
}

// RoundPrice snaps positive prices down for buys and up for sells using an
// integer quotient/remainder, independently of decimal.DivisionPrecision.
// A rounded zero is possible; order validation must recheck positivity and the
// instrument's minimum notional after snapping, before reserving or submitting.
func RoundPrice(price, tick decimal.Decimal, side Side) (decimal.Decimal, error) {
	if side != SideBuy && side != SideSell {
		return decimal.Zero, ErrInvalidSide
	}
	if !boundedPositive(price) || !boundedPositive(tick) {
		return decimal.Zero, ErrInvalidIncrement
	}
	quotient, remainder := price.QuoRem(tick, 0)
	if side == SideSell && !remainder.IsZero() {
		quotient = quotient.Add(decimal.NewFromInt(1))
	}
	return quotient.Mul(tick), nil
}

// RoundQuantity snaps a positive quantity down to a positive lot. Callers must
// recheck a zero result and the order's minimum notional after rounding.
func RoundQuantity(quantity, lot decimal.Decimal) (decimal.Decimal, error) {
	if !boundedPositive(quantity) || !boundedPositive(lot) {
		return decimal.Zero, ErrInvalidIncrement
	}
	quotient, _ := quantity.QuoRem(lot, 0)
	return quotient.Mul(lot), nil
}

func decimalSyntax(input string, rule DecimalRule) bool {
	if input == "" || len(input) > rule.IntegerDigits+rule.Scale+2 {
		return false
	}
	if input[0] == '-' {
		if !rule.AllowNegative {
			return false
		}
		input = input[1:]
	}
	whole, fraction, dot := strings.Cut(input, ".")
	if len(whole) > rule.IntegerDigits || !asciiDigits(whole) || len(whole) > 1 && whole[0] == '0' {
		return false
	}
	return !dot || len(fraction) <= rule.Scale && asciiDigits(fraction)
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func boundedPositive(value decimal.Decimal) bool {
	// Parsed operands have at most 38+38 digits. Allow one multiplication's
	// precision growth while rejecting pathological internal exponent values.
	return value.IsPositive() && value.Exponent() >= -76 && value.Exponent() <= 76 && len(value.Coefficient().String()) <= 152
}
