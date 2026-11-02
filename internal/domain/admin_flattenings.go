package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// AdminFlattenScope confirms the account inventory, never an invented strategy share.
type AdminFlattenScope string

// AdminFlattenPlanKind describes a proposal, not a durable execution outcome.
type AdminFlattenPlanKind string

// Flattening is a separately restricted simulation operation, not stop or cancel.
const (
	AdminFlattenScopeAccountInstrument AdminFlattenScope    = "account_instrument"
	AdminFlattenNoOrder                AdminFlattenPlanKind = "NO_ORDER"
	AdminFlattenSell                   AdminFlattenPlanKind = "REDUCE_ONLY_SELL"
	AdminFlattenAction                                      = "account.flatten"
	AdminFlattenAuditResource                               = "account_instrument"
	MessageInvalidAdminFlatten                              = "invalid account flattening confirmation"
	MessageAdminFlattenDenied                               = "account flattening conditions are not met"
)

// Flatten failures never include submitted business or storage details.
var (
	ErrInvalidAdminFlatten = errors.New(MessageInvalidAdminFlatten)
	ErrAdminFlattenDenied  = errors.New(MessageAdminFlattenDenied)
)

// AdminFlattenRequest binds a separate confirmation to a completed cancellation
// and current account observations. Decimal wire fields must be strings; this
// internal value is already parsed without floating point.
type AdminFlattenRequest struct {
	Key                       string
	Scope                     AdminFlattenScope
	StrategyID                int64
	AccountID                 string
	InstrumentID              int64
	CancellationID            int64
	ExpectedRiskRevision      int64
	ExpectedPortfolioRevision int64
	ExpectedBaseRevision      int64
	ConfirmedQuantity         decimal.Decimal
	LimitPrice                decimal.Decimal
	Reason                    string
}

// AdminFlattenBasis is a proposed internal snapshot. It does not authenticate
// the caller or prove its own freshness; persistence must re-read and lock the
// actual role, identities, obligations, source facts and balances atomically.
type AdminFlattenBasis struct {
	Role                 AdminRole
	ObservedAt           time.Time
	StrategyID           int64
	AccountID            string
	InstrumentID         int64
	ExecutionVenue       string
	StrategyCount        int64
	RiskEnabled          bool
	RiskRevision         int64
	PortfolioRevision    int64
	BaseRevision         int64
	ProjectionReconciled bool
	SourceComplete       bool
	OpenObligations      int64
	PendingSubmissions   int64
	PortfolioQuantity    decimal.Decimal
	CoreAvailable        decimal.Decimal
	CoreFrozen           decimal.Decimal
	TickSize             decimal.Decimal
	LotSize              decimal.Decimal
	MinQuantity          decimal.Decimal
	MinNotional          decimal.Decimal
	MaxOrderNotional     decimal.Decimal
	Cancellation         AdminEmergencyCancelRecord
}

// AdminFlattenPlan deliberately lacks an idempotent intent identity, lease and
// execution authority. Neither a sale proposal nor NO_ORDER completes a task.
type AdminFlattenPlan struct {
	Kind        AdminFlattenPlanKind
	Side        Side
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	Notional    decimal.Decimal
	ReduceOnly  bool
	TimeInForce TimeInForce
}

// Normalize requires explicit account scope and exact independent revisions.
// It does not increment or close risk, persist work, or authorize execution.
func (r AdminFlattenRequest) Normalize() (AdminFlattenRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Scope != AdminFlattenScopeAccountInstrument || r.StrategyID <= 0 || !intentIdentityPart(r.AccountID, 64) || r.InstrumentID <= 0 || r.CancellationID <= 0 ||
		r.ExpectedRiskRevision <= 0 || r.ExpectedPortfolioRevision <= 0 || r.ExpectedBaseRevision <= 0 ||
		len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) || len(r.Reason) == 0 || len(r.Reason) > 256 || !utf8.ValidString(r.Reason) || strings.ContainsRune(r.Reason, 0) ||
		!FixedDecimal(r.ConfirmedQuantity, 40, 36, true) || !FixedDecimal(r.LimitPrice, 20, 18, false) {
		return AdminFlattenRequest{}, ErrInvalidAdminFlatten
	}
	return r, nil
}

// PlanAdminFlatten only supports one strategy bound to one simulated spot
// account/instrument. It rejects shared accounts, stale confirmations, unknown
// submits and every residual obligation before proposing a reduce-only sale.
func PlanAdminFlatten(request AdminFlattenRequest, basis AdminFlattenBasis) (AdminFlattenPlan, error) {
	r, err := request.Normalize()
	if err != nil {
		return AdminFlattenPlan{}, err
	}
	if !flattenBasisMatches(r, basis) || !flattenCancellationMatches(r, basis.Cancellation, basis.ObservedAt) || !flattenMetadataValid(basis) {
		return AdminFlattenPlan{}, ErrAdminFlattenDenied
	}
	price, err := RoundPrice(r.LimitPrice, basis.TickSize, SideSell)
	if err != nil || !price.Equal(r.LimitPrice) {
		return AdminFlattenPlan{}, ErrAdminFlattenDenied
	}
	if r.ConfirmedQuantity.IsZero() {
		return AdminFlattenPlan{Kind: AdminFlattenNoOrder}, nil
	}
	return flattenSale(r, basis)
}

func flattenBasisMatches(r AdminFlattenRequest, b AdminFlattenBasis) bool {
	return b.Role == AdminRoleAdmin && b.ExecutionVenue == ExecutionVenueSimulator && b.StrategyCount == 1 &&
		b.StrategyID == r.StrategyID && b.AccountID == r.AccountID && b.InstrumentID == r.InstrumentID && !b.RiskEnabled &&
		b.RiskRevision == r.ExpectedRiskRevision && b.PortfolioRevision == r.ExpectedPortfolioRevision && b.BaseRevision == r.ExpectedBaseRevision &&
		b.ProjectionReconciled && b.SourceComplete && b.OpenObligations == 0 && b.PendingSubmissions == 0 &&
		FixedDecimal(b.CoreAvailable, 40, 36, true) && FixedDecimal(b.CoreFrozen, 40, 36, true) && FixedDecimal(b.PortfolioQuantity, 40, 36, true) &&
		b.CoreFrozen.IsZero() && b.CoreAvailable.Equal(r.ConfirmedQuantity) && b.PortfolioQuantity.Equal(r.ConfirmedQuantity)
}

func flattenCancellationMatches(r AdminFlattenRequest, record AdminEmergencyCancelRecord, observed time.Time) bool {
	a := record.Acceptance
	return a.ID == r.CancellationID && a.StrategyID == r.StrategyID && a.Scope == AdminEmergencyCancelScopeStrategy &&
		a.ExpectedRiskRevision > 0 && a.RiskRevision == r.ExpectedRiskRevision && a.ExpectedRiskRevision == a.RiskRevision-1 &&
		record.State == AdminEmergencyCancelSucceeded && record.Revision > 0 && record.EnrollmentComplete &&
		a.ActorUserID > 0 && a.CreatedAt.UnixMilli() > 0 && observed.UnixMilli() > 0 && !record.CompletedAt.IsZero() &&
		!record.CompletedAt.Before(a.CreatedAt) && !record.CompletedAt.After(observed)
}

func flattenMetadataValid(b AdminFlattenBasis) bool {
	return FixedDecimal(b.TickSize, 20, 18, false) && FixedDecimal(b.LotSize, 20, 18, false) &&
		FixedDecimal(b.MinQuantity, 20, 18, false) && FixedDecimal(b.MinNotional, 40, 36, false) && FixedDecimal(b.MaxOrderNotional, 40, 36, false)
}

func flattenSale(r AdminFlattenRequest, b AdminFlattenBasis) (AdminFlattenPlan, error) {
	// Ledger numeric columns retain 36 places. Rescaling is allowed only when
	// numerically identical; a nonzero remainder must never disappear as dust.
	orderQuantity := r.ConfirmedQuantity.Truncate(18)
	if !orderQuantity.Equal(r.ConfirmedQuantity) || !FixedDecimal(orderQuantity, 20, 18, false) || orderQuantity.LessThan(b.MinQuantity) {
		return AdminFlattenPlan{}, ErrAdminFlattenDenied
	}
	quantity, err := RoundQuantity(orderQuantity, b.LotSize)
	if err != nil || !quantity.Equal(r.ConfirmedQuantity) {
		return AdminFlattenPlan{}, ErrAdminFlattenDenied
	}
	notional := r.LimitPrice.Mul(quantity)
	if !FixedDecimal(notional, 40, 36, false) || notional.LessThan(b.MinNotional) || notional.GreaterThan(b.MaxOrderNotional) {
		return AdminFlattenPlan{}, ErrAdminFlattenDenied
	}
	return AdminFlattenPlan{
		Kind:        AdminFlattenSell,
		Side:        SideSell,
		Price:       r.LimitPrice,
		Quantity:    quantity,
		Notional:    notional,
		ReduceOnly:  true,
		TimeInForce: TimeInForceGTC,
	}, nil
}
