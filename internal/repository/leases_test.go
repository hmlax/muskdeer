package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestInvalidLeaseRequestsCannotReachDatabase(t *testing.T) {
	store, err := NewLeases(&gorm.DB{}) // No connection: valid requests would fail.
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "lease-unit")
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "account-a",
	}
	for _, invalidTTL := range []time.Duration{0, domain.MinLeaseTTL - time.Millisecond, domain.MaxLeaseTTL + time.Millisecond, domain.MinLeaseTTL + time.Nanosecond} {
		if _, err := store.Acquire(ctx, key, owner, invalidTTL); !errors.Is(err, domain.ErrInvalidLease) {
			t.Fatalf("invalid TTL reached database: %v", err)
		}
	}
	if _, err := store.Acquire(ctx, key, "reusable-node-name", time.Second); !errors.Is(err, domain.ErrInvalidLease) {
		t.Fatalf("owner without incarnation nonce was accepted: %v", err)
	}
	if _, err := store.Acquire(context.WithoutCancel(ctx), key, owner, time.Second); !errors.Is(err, domain.ErrInvalidLease) {
		t.Fatalf("unbounded lease work was accepted: %v", err)
	}
	cancel()
	if _, err := store.Acquire(ctx, key, owner, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost its cause: %v", err)
	}
}
