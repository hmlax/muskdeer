package native

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

type retainedLeases struct {
	domain.LeaseRepository
	token    domain.LeaseToken
	err      error
	renewals int
}

func (r *retainedLeases) Acquire(context.Context, domain.LeaseKey, string, time.Duration) (domain.LeaseToken, error) {
	return r.token, r.err
}

func (r *retainedLeases) Renew(_ context.Context, token domain.LeaseToken, ttl time.Duration) (domain.LeaseToken, error) {
	r.renewals++
	token.ExpiresAt = time.Now().Add(ttl)
	return token, nil
}

func TestKeepLeaseRenewsOnlyNearExpiryAndPreservesOwnershipErrors(t *testing.T) {
	key := domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "retained-fund",
	}
	for _, remaining := range []time.Duration{9 * time.Second, time.Second} {
		store := &retainedLeases{token: domain.LeaseToken{
			Key:       key,
			OwnerID:   "test-owner",
			Epoch:     7,
			ExpiresAt: time.Now().Add(remaining),
		}}
		token, err := KeepLease(t.Context(), store, key, store.token.OwnerID, 10*time.Second)
		if err != nil || token.Epoch != store.token.Epoch || token.OwnerID != store.token.OwnerID {
			t.Fatalf("retained lease changed owner: %v, %v", token, err)
		}
		want := 0
		if remaining < 5*time.Second {
			want = 1
		}
		if store.renewals != want {
			t.Fatalf("renewals=%d, want %d", store.renewals, want)
		}
	}
	store := &retainedLeases{err: domain.ErrLeaseHeld}
	if _, err := KeepLease(t.Context(), store, key, "test-owner", time.Second); !errors.Is(err, domain.ErrLeaseHeld) || store.renewals != 0 {
		t.Fatalf("foreign lease accepted: %v", err)
	}
}
