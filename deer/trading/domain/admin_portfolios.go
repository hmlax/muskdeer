package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AdminPortfolioFilter selects initialized account projections, never an
// invented share of an account assigned to one strategy.
type AdminPortfolioFilter struct {
	Page         AdminPage
	AccountID    string
	InstrumentID int64
}

// Validate bounds pagination and requires an exact business account key.
func (f AdminPortfolioFilter) Validate() error {
	if f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize || f.InstrumentID < 0 ||
		f.AccountID != "" && !intentIdentityPart(f.AccountID, 64) {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminPortfolioBalance is an independent authoritative core observation.
// Missing balance rows remain absent, not synthetic zero funds.
type AdminPortfolioBalance struct {
	ID        int64
	Asset     string
	Available decimal.Decimal
	Frozen    decimal.Decimal
	Revision  int64
	UpdatedAt time.Time
}

// AdminPortfolioFacts summarizes observed source IDs, not a reconciliation proof.
// Source IDs are global and may have gaps; Count is not a sequence number.
type AdminPortfolioFacts struct {
	Count                int64
	LatestSourceSequence int64
}

// AdminPortfolio shows a stored, rebuildable account projection separately from
// current source/collection counts and core funds. Projection.Reconciled is the
// stored producer diagnostic and can be older than the independent observations.
// Reads do not certify current reconciliation, compute a market valuation, or
// allow new risk. The account includes all of its strategies.
type AdminPortfolio struct {
	ID               int64
	ExecutionVenue   string
	Symbol           string
	AccountEnabled   bool
	AccountRevision  int64
	Projection       PortfolioSnapshot
	Source           AdminPortfolioFacts
	Collected        AdminPortfolioFacts
	CoreBaseBalance  *AdminPortfolioBalance
	CoreQuoteBalance *AdminPortfolioBalance
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AdminPortfolioPage pages the persisted projection identity in ascending order.
type AdminPortfolioPage struct {
	Items       []AdminPortfolio
	NextAfterID int64
}

// AdminPortfolios authenticates the current viewer in a read-only snapshot.
// Reads never call the mutating portfolio Get/Rebuild/Apply paths, repair funds,
// update reconciliation flags or release OMS reservations.
type AdminPortfolios interface {
	List(context.Context, string, AdminPortfolioFilter) (AdminPortfolioPage, error)
	Detail(context.Context, string, int64) (AdminPortfolio, error)
}
