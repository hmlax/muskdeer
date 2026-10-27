package domain

import (
	"context"
	"errors"
	"time"
)

// LeaseKind separates strategy, account execution and public market ownership.
type LeaseKind string

// Lease kinds and allowed renewal durations.
const (
	LeaseKindStrategy     LeaseKind = "strategy"
	LeaseKindExecution    LeaseKind = "execution"
	LeaseKindMarket       LeaseKind = "market"
	MinLeaseTTL                     = 100 * time.Millisecond
	MaxLeaseTTL                     = 30 * time.Second
	MessageLeaseHeld                = "lease is held by another owner"
	MessageLeaseLost                = "lease authority is no longer current"
	MessageInvalidLease             = "invalid lease request"
	MessageEpochExhausted           = "lease epoch cannot advance"
)

// Coordination errors fail admission rather than extending unknown authority.
var (
	ErrLeaseHeld      = errors.New(MessageLeaseHeld)
	ErrLeaseLost      = errors.New(MessageLeaseLost)
	ErrInvalidLease   = errors.New(MessageInvalidLease)
	ErrEpochExhausted = errors.New(MessageEpochExhausted)
)

// LeaseKey selects exactly one strategy, account or market feed resource.
type LeaseKey struct {
	Kind         LeaseKind
	StrategyID   int64
	AccountID    string
	MarketFeedID int64
}

// LeaseToken contains a proposed authority epoch. ExpiresAt is informational;
// irreversible effects must compare the persisted lease using database time.
type LeaseToken struct {
	Key       LeaseKey
	OwnerID   string
	Epoch     int64
	ExpiresAt time.Time
}

// LeaseRepository owns bounded database-clock acquisition and renewal. OwnerID
// must be a new incarnation identity; reusable process/service names are unsafe.
// This interface does not authorize an effect after a separate lease lookup.
type LeaseRepository interface {
	Acquire(ctx context.Context, key LeaseKey, ownerID string, ttl time.Duration) (LeaseToken, error)
	Renew(ctx context.Context, token LeaseToken, ttl time.Duration) (LeaseToken, error)
	Release(ctx context.Context, token LeaseToken) error
}
