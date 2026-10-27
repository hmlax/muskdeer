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

func TestInstanceUsesValidatedIncarnationAndLastSeparator(t *testing.T) {
	owner, err := New(t.Context(), "execution.node.a")
	if err != nil {
		t.Fatal(err)
	}
	if instance, valid := Instance(owner); !valid || instance != "execution.node.a" {
		t.Fatal("dotted instance prefix was changed")
	}
	for _, value := range []string{"", "execution.node.a", "node.not-a-nonce", ".01234567890123456789012345678901", "bad/node.01234567890123456789012345678901"} {
		if instance, valid := Instance(value); valid || instance != "" {
			t.Fatal("unvalidated incarnation became a routing target")
		}
	}
}
