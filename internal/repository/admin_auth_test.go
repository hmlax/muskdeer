package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminHashAdmissionIsBoundedAndCancellationPrecedesWork(t *testing.T) {
	auth := &adminAuth{hashSlots: make(chan struct{}, adminHashConcurrency)}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for range adminHashConcurrency {
		if err := auth.acquireHash(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := auth.acquireHash(ctx); !errors.Is(err, domain.ErrAuthUnavailable) {
		t.Fatal("hash workload queued without a bound")
	}
	for range adminHashConcurrency {
		auth.releaseHash()
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := auth.acquireHash(canceled); !errors.Is(err, context.Canceled) || len(auth.hashSlots) != 0 {
		t.Fatal("canceled authentication consumed a hash slot")
	}
}

func TestAdminSecretsAreCanonicalAndErrorsDoNotExposePersistenceDetails(t *testing.T) {
	secret, err := adminSecret()
	if err != nil {
		t.Fatal(err)
	}
	if digest, valid := adminSecretHash(secret); !valid || len(digest) != 64 || digest == secret {
		t.Fatal("random token did not map to a fixed digest")
	}
	for _, invalid := range []string{"", secret + "=", strings.Repeat("A", 42) + "B", strings.Repeat("+", 43), strings.Repeat("A", 44)} {
		if _, valid := adminSecretHash(invalid); valid {
			t.Fatal("noncanonical bearer accepted")
		}
	}
	private := errors.New("private database password or bearer")
	wrapped := adminAuthFailure(private)
	if wrapped.Error() != domain.MessageAuthUnavailable || !errors.Is(wrapped, private) || !errors.Is(wrapped, domain.ErrAuthUnavailable) {
		t.Fatal("persistence error leaked or lost its cause")
	}
	for _, value := range []string{"xx", "has space", "_wrong-first", strings.Repeat("a", 65), "用户名称"} {
		if _, valid := adminUsername(value); valid {
			t.Fatal("unsafe identity syntax accepted")
		}
	}
}
