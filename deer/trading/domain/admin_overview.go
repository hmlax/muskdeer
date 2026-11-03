package domain

import (
	"context"
	"time"
)

// AdminOverviewFilter selects stored strategies. Counts cover this whole scope,
// independently of the bounded ascending page cursor. Source selects requested
// market_data_source; active configuration/source remain separate row fields.
type AdminOverviewFilter struct {
	Page      AdminPage
	AccountID string
	Source    MarketSource
}

// Validate requires exact account/source values and a bounded page.
func (f AdminOverviewFilter) Validate() error {
	if f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize ||
		f.AccountID != "" && !intentIdentityPart(f.AccountID, 64) ||
		f.Source != MarketSourceUnspecified && f.Source.ValidateSelection() != nil {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminStoredStateCount counts persisted states, without claiming live workers.
type AdminStoredStateCount struct {
	State StrategyState
	Count int64
}

// AdminLeaseObservation is a retained lease sampled at database ObservedAt.
// OwnerRecorded includes released holders; release retains owner/epoch and clears
// expiry. A zero-epoch slot can exist without a holder. An unexpired matching
// lease is not process health or final order authority.
type AdminLeaseObservation struct {
	Epoch                  int64
	OwnerRecorded          bool
	ExpiresAt              time.Time
	UnexpiredAtObservation bool
	RuntimeMatchesLease    bool
}

// AdminStrategyRuntime separates stored metadata from an optional retained lease.
type AdminStrategyRuntime struct {
	Strategy AdminStrategy
	Lease    *AdminLeaseObservation
}

// AdminStrategyOverview has one authorized read-only database observation.
// RequestedRunning and StoredStates describe different facts; neither proves
// current market quality, reconciliation, process health or permission to trade.
type AdminStrategyOverview struct {
	ObservedAt       time.Time
	Total            int64
	RequestedRunning int64
	StoredStates     []AdminStoredStateCount
	Items            []AdminStrategyRuntime
	NextAfterID      int64
}

// AdminOverview reads metadata/counts/leases without renewing or acquiring
// ownership, checking providers, rebuilding positions or changing risk.
type AdminOverview interface {
	Strategies(context.Context, string, AdminOverviewFilter) (AdminStrategyOverview, error)
}
