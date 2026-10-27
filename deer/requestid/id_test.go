package requestid

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRequestIDValidationAndContextLifetime(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	accepted, err := New(ctx, "edge-a.1_2-3")
	if err != nil || accepted.String() != "edge-a.1_2-3" {
		t.Fatalf("valid request ID = %v, %v", accepted, err)
	}
	bound := Bind(ctx, accepted)
	if FromContext(bound) != accepted {
		t.Fatal("typed metadata did not survive context binding")
	}
	before, _ := ctx.Deadline()
	after, _ := bound.Deadline()
	if before != after {
		t.Fatal("binding changed the native deadline")
	}
	cancel()
	if !errors.Is(bound.Err(), context.Canceled) {
		t.Fatal("binding detached caller cancellation")
	}
}

func TestInvalidIDsGenerateBoundedIndependentValues(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	for _, candidate := range []string{"", "bad\nheader", "a,b", "空值", strings.Repeat("a", 65)} {
		id, err := New(t.Context(), candidate)
		if err != nil || len(id.String()) != 32 || id.String() == candidate {
			t.Fatalf("unsafe request ID normalization: length=%d error=%v", len(id.String()), err)
		}
		if _, err := hex.DecodeString(id.String()); err != nil {
			t.Fatal(err)
		}
		if _, duplicate := seen[id.String()]; duplicate {
			t.Fatal("independent requests reused one generated ID")
		}
		seen[id.String()] = struct{}{}
	}
}
