package muskdeer

import "testing"

func TestTradingIDsAndCanonicalUserScope(t *testing.T) {
	user, ok := TradingID("9223372036854775807")
	if !ok || user != 9223372036854775807 {
		t.Fatalf("TradingID(max int64) = %d, %v", user, ok)
	}
	for _, value := range []string{"0", "01", "-1", "1e3", "9223372036854775808"} {
		if _, ok := TradingID(value); ok {
			t.Fatalf("TradingID(%q) accepted", value)
		}
	}
	for _, value := range []string{"himalaya/12/3", "himalaya/123/3"} {
		if _, ok := AccountUser(value); !ok {
			t.Fatalf("AccountUser(%q) rejected canonical scope", value)
		}
	}
	if UserScopePrefix(12) == UserScopePrefix(123) {
		t.Fatal("different user prefixes overlap")
	}
	if _, ok := AccountUser("himalaya/12/03"); ok {
		t.Fatal("noncanonical fund accepted")
	}
}
