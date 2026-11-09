package native

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// KeepLease reuses a retained database lease until half its lifetime remains.
// This is scheduling only: final effects still lock and fence the exact epoch
// with database time. A concurrently released token cannot authorize a write.
func KeepLease(ctx context.Context, leases domain.LeaseRepository, key domain.LeaseKey, owner string, ttl time.Duration) (domain.LeaseToken, error) {
	token, err := leases.Acquire(ctx, key, owner, ttl)
	if err != nil {
		return domain.LeaseToken{}, err
	}
	if time.Until(token.ExpiresAt) > ttl/2 {
		return token, nil
	}
	return leases.Renew(ctx, token, ttl)
}
