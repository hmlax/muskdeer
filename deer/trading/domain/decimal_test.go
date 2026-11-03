package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestDecimalBoundaryPreservesPrecisionAndRejectsUnspecifiedFormats(t *testing.T) {
	rule := DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	for _, input := range []string{"0.1", "0.2", "99999999999999999999.999999999999999999"} {
		value, err := ParseDecimal(input, rule)
		if err != nil || value.String() != input {
			t.Fatalf("parse %s = %s, %v", input, value, err)
		}
	}
	for _, input := range []string{"", " 1", "1 ", "+1", "01", "1.", ".1", "1e3", "NaN", "Inf", "-1", "0", "0.0000000000000000001", "100000000000000000000", "1.2.3", "１２"} {
		if _, err := ParseDecimal(input, rule); !errors.Is(err, ErrInvalidDecimal) {
			t.Fatalf("invalid decimal %q = %v", input, err)
		}
	}
	left, err := ParseDecimal("0.1", rule)
	if err != nil {
		t.Fatal(err)
	}
	right, err := ParseDecimal("0.2", rule)
	if err != nil {
		t.Fatal(err)
	}
	if result := left.Mul(right).String(); result != "0.02" {
		t.Fatalf("exact product = %s", result)
	}
	if _, err := ParseDecimal("1", DecimalRule{}); !errors.Is(err, ErrInvalidDecimalRule) {
		t.Fatalf("unspecified precision was accepted: %v", err)
	}
}

func TestSignedValuesAndZeroRequireExplicitPolicy(t *testing.T) {
	rule := DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
		AllowNegative: true,
		AllowZero:     true,
	}
	for _, input := range []string{"-123.45", "0", "-0.000", "0.000"} {
		if _, err := ParseDecimal(input, rule); err != nil {
			t.Fatalf("signed/zero value %s = %v", input, err)
		}
	}
}

func TestTickAndLotQuantizationDoesNotRoundDivisionBeforeSnapping(t *testing.T) {
	price := decimal.RequireFromString("1.000000000000000001")
	tick := decimal.RequireFromString("0.000000000000000003")
	for _, test := range []struct {
		side Side
		want string
	}{
		{
			side: SideBuy,
			want: "0.999999999999999999",
		},
		{
			side: SideSell,
			want: "1.000000000000000002",
		},
	} {
		actual, err := RoundPrice(price, tick, test.side)
		if err != nil || actual.String() != test.want || !actual.Mod(tick).IsZero() {
			t.Fatalf("price for %s = %s, %v", test.side, actual, err)
		}
	}
	quantity, err := RoundQuantity(decimal.RequireFromString("0.300000000000000001"), decimal.RequireFromString("0.1"))
	if err != nil || quantity.String() != "0.3" {
		t.Fatalf("lot quantity = %s, %v", quantity, err)
	}
	if _, err := RoundPrice(price, decimal.Zero, SideBuy); !errors.Is(err, ErrInvalidIncrement) {
		t.Fatalf("zero tick did not fail safely: %v", err)
	}
	if _, err := RoundPrice(price, tick, SideUnspecified); !errors.Is(err, ErrInvalidSide) {
		t.Fatalf("unknown side = %v", err)
	}
}
