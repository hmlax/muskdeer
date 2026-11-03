package domain

import (
	"errors"
	"testing"
)

func TestIntentClientIdentityRetainsHistoricalFormulaAndAccountScope(t *testing.T) {
	identity, err := IntentClientOrderID("account", "intent-1")
	if err != nil || identity != "922949274e6ab99215bc8f6f835084fc46d6237e080ba8486c43eae7fc276189" {
		t.Fatalf("historical client identity changed: %s, %v", identity, err)
	}
	other, err := IntentClientOrderID("other-account", "intent-1")
	if err != nil || other == identity {
		t.Fatal("account scopes shared an execution identity")
	}
	for _, key := range []string{"", "invalid key", "key\nother"} {
		if _, err := IntentClientOrderID("account", key); !errors.Is(err, ErrInvalidOrder) {
			t.Fatal("ambiguous client identity accepted")
		}
	}
}
