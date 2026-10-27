package usecase

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

// ExecutionOwner proposes this process's current account shard lease. The
// simulator still verifies owner/epoch inside the effect transaction.
type ExecutionOwner struct {
	store domain.LeaseRepository
	owner string
	ttl   time.Duration
}

// NewExecutionOwner borrows a lease repository and owns no local authority map.
func NewExecutionOwner(store domain.LeaseRepository, owner string, ttl time.Duration) (*ExecutionOwner, error) {
	if store == nil || !ownerid.Valid(owner) || ttl < domain.MinLeaseTTL || ttl > domain.MaxLeaseTTL || ttl%time.Millisecond != 0 {
		return nil, domain.ErrInvalidLease
	}
	return &ExecutionOwner{
		store: store,
		owner: owner,
		ttl:   ttl,
	}, nil
}

// ForAccount acquires or refreshes one lease using database time. An uncertain
// or expired renewal returns an error, without attempting the trading effect.
func (o *ExecutionOwner) ForAccount(ctx context.Context, accountID string) (domain.LeaseToken, error) {
	token, err := o.store.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: accountID,
	}, o.owner, o.ttl)
	if err != nil {
		return domain.LeaseToken{}, err
	}
	return o.store.Renew(ctx, token, o.ttl)
}
