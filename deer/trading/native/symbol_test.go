package native

import "testing"

func TestSymbolCatalogNames(t *testing.T) {
	for _, value := range []string{"BTCUSDT", "1000PEPEUSDT", "币安人生USDT"} {
		if !Symbol(value) {
			t.Errorf("catalog symbol rejected: %q", value)
		}
	}
	for _, value := range []string{"btcUSDT", "BTC/USDT", "币安 人生USDT", "BTC\u202eUSDT", "BTC?x=1", "币安人生币安人生币安人生USDT"} {
		if Symbol(value) {
			t.Errorf("unsafe or oversized symbol accepted: %q", value)
		}
	}
}
