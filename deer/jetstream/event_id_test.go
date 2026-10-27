package jetstream

import (
	"strings"
	"testing"
)

func TestMessageIDAllowsStableBusinessKeysWithoutHeaderInjection(t *testing.T) {
	for _, value := range []string{"intent:9223372036854775807:9223372036854775807", "fill:simulator:1", strings.Repeat("x", 128)} {
		if !validID(value) {
			t.Fatalf("stable business identity rejected: %q", value)
		}
	}
	for _, value := range []string{"", "x\r\nNats-Msg-Id: injected", "x y", "x\t", "x.*", "é", strings.Repeat("x", 129)} {
		if validID(value) {
			t.Fatalf("unsafe message header identity accepted: %q", value)
		}
	}
}
