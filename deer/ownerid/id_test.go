package ownerid

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnerIncarnationsChangeOnRestartAndRespectCancellation(t *testing.T) {
	first, err := New(t.Context(), "strategy-node")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.Context(), "strategy-node")
	if err != nil || first == second || !strings.HasPrefix(first, "strategy-node.") || len(first) != len("strategy-node.")+32 {
		t.Fatalf("process incarnation identities = %s, %s, %v", first, second, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New(ctx, "strategy-node"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled incarnation = %v", err)
	}
	if _, err := New(t.Context(), "unsafe\nnode"); err == nil {
		t.Fatal("unsafe owner prefix accepted")
	}
}
