package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func FuzzDecimalSnappingPreservesBounds(f *testing.F) {
	for _, input := range []string{"0.1", "-1.000000000000000001", "0", "99999999999999999999.999999999999999999", "1e99999999", "+1", "0.000000000000000003"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		value, err := ParseDecimal(input, DecimalRule{
			IntegerDigits: 20,
			Scale:         18,
			AllowNegative: true,
			AllowZero:     true,
		})
		if err != nil {
			return
		}
		roundtrip, err := ParseDecimal(value.String(), DecimalRule{
			IntegerDigits: 20,
			Scale:         18,
			AllowNegative: true,
			AllowZero:     true,
		})
		if err != nil || !roundtrip.Equal(value) {
			t.Fatalf("decimal normalization changed value: %s, %v", value, err)
		}
		if value.IsZero() {
			return
		}
		price := value.Abs()
		tick := decimal.RequireFromString("0.000000000000000003")
		buy, err := RoundPrice(price, tick, SideBuy)
		if err != nil {
			t.Fatal(err)
		}
		sell, err := RoundPrice(price, tick, SideSell)
		if err != nil {
			t.Fatal(err)
		}
		if buy.GreaterThan(price) || sell.LessThan(price) || sell.Sub(buy).GreaterThan(tick) || !buy.Mod(tick).IsZero() || !sell.Mod(tick).IsZero() {
			t.Fatalf("invalid rounding bounds: buy=%s price=%s sell=%s", buy, price, sell)
		}
	})
}
