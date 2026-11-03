package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestAdminFlattenRequiresExplicitAccountInstrumentAndExactConfirmation(t *testing.T) {
	t.Parallel()
	request, _ := flattenDomainFixture()
	request.Reason = "  explicitly flatten this isolated simulated account  "
	normalized, err := request.Normalize()
	if err != nil || normalized.StrategyID != request.StrategyID || normalized.InstrumentID != request.InstrumentID || normalized.ExpectedRiskRevision != request.ExpectedRiskRevision ||
		normalized.ExpectedPortfolioRevision != request.ExpectedPortfolioRevision || normalized.ExpectedBaseRevision != request.ExpectedBaseRevision || normalized.Reason != strings.TrimSpace(request.Reason) || !normalized.ConfirmedQuantity.Equal(request.ConfirmedQuantity) {
		t.Fatal("flatten confirmation lost identity, decimal or reason", err)
	}
	for _, mutate := range []func(*AdminFlattenRequest){
		func(r *AdminFlattenRequest) { r.Scope = "" },
		func(r *AdminFlattenRequest) { r.Scope = "strategy" },
		func(r *AdminFlattenRequest) { r.Scope = "account" },
		func(r *AdminFlattenRequest) { r.Scope = "all" },
		func(r *AdminFlattenRequest) { r.StrategyID = 0 },
		func(r *AdminFlattenRequest) { r.AccountID = "invalid account" },
		func(r *AdminFlattenRequest) { r.InstrumentID = 0 },
		func(r *AdminFlattenRequest) { r.CancellationID = 0 },
		func(r *AdminFlattenRequest) { r.ExpectedRiskRevision = 0 },
		func(r *AdminFlattenRequest) { r.ExpectedPortfolioRevision = 0 },
		func(r *AdminFlattenRequest) { r.ExpectedBaseRevision = 0 },
		func(r *AdminFlattenRequest) { r.Key = "short" },
		func(r *AdminFlattenRequest) { r.Key = strings.Repeat("k", 65) },
		func(r *AdminFlattenRequest) { r.Key = "invalid/key/with/slash" },
		func(r *AdminFlattenRequest) { r.Reason = " \t\n" },
		func(r *AdminFlattenRequest) { r.Reason = strings.Repeat("界", 86) },
		func(r *AdminFlattenRequest) { r.Reason = "unsafe\x00reason" },
		func(r *AdminFlattenRequest) { r.Reason = string([]byte{0xff}) },
		func(r *AdminFlattenRequest) { r.LimitPrice = decimal.Zero },
		func(r *AdminFlattenRequest) { r.LimitPrice = decimal.New(1, -19) },
		func(r *AdminFlattenRequest) { r.ConfirmedQuantity = decimal.NewFromInt(-1) },
		func(r *AdminFlattenRequest) { r.ConfirmedQuantity = decimal.New(1, -37) },
	} {
		candidate := request
		mutate(&candidate)
		if _, err := candidate.Normalize(); !errors.Is(err, ErrInvalidAdminFlatten) {
			t.Fatal("invalid or expanded flatten confirmation accepted", err)
		}
	}
}

func TestAdminFlattenPlansOnlyExactReduceOnlySpotSale(t *testing.T) {
	t.Parallel()
	request, basis := flattenDomainFixture()
	plan, err := PlanAdminFlatten(request, basis)
	if err != nil || plan.Kind != AdminFlattenSell || plan.Side != SideSell || !plan.ReduceOnly || plan.TimeInForce != TimeInForceGTC ||
		!plan.Price.Equal(request.LimitPrice) || !plan.Quantity.Equal(request.ConfirmedQuantity) || !plan.Notional.Equal(decimal.RequireFromString("123.46913480236913477891")) {
		t.Fatal("flatten plan rounded, enlarged or changed the confirmed sale", err)
	}
	request.ExpectedRiskRevision = math.MaxInt64
	basis.RiskRevision = math.MaxInt64
	basis.Cancellation.Acceptance.ExpectedRiskRevision = math.MaxInt64 - 1
	basis.Cancellation.Acceptance.RiskRevision = math.MaxInt64
	if _, err := PlanAdminFlatten(request, basis); err != nil {
		t.Fatal("non-incrementing risk confirmation lost its int64 upper bound", err)
	}
}

func TestAdminFlattenRejectsSharedStaleUnreconciledAndUnresolvedFacts(t *testing.T) {
	t.Parallel()
	request, valid := flattenDomainFixture()
	for _, mutate := range []func(*AdminFlattenBasis){
		func(b *AdminFlattenBasis) { b.Role = AdminRoleViewer },
		func(b *AdminFlattenBasis) { b.Role = AdminRoleOperator },
		func(b *AdminFlattenBasis) { b.Role = "unknown" },
		func(b *AdminFlattenBasis) { b.ObservedAt = time.Time{} },
		func(b *AdminFlattenBasis) { b.ExecutionVenue = "real-venue" },
		func(b *AdminFlattenBasis) { b.StrategyCount = 0 },
		func(b *AdminFlattenBasis) { b.StrategyCount = 2 },
		func(b *AdminFlattenBasis) { b.StrategyID++ },
		func(b *AdminFlattenBasis) { b.AccountID = "another-account" },
		func(b *AdminFlattenBasis) { b.InstrumentID++ },
		func(b *AdminFlattenBasis) { b.RiskEnabled = true },
		func(b *AdminFlattenBasis) { b.RiskRevision-- },
		func(b *AdminFlattenBasis) { b.PortfolioRevision-- },
		func(b *AdminFlattenBasis) { b.BaseRevision-- },
		func(b *AdminFlattenBasis) { b.ProjectionReconciled = false },
		func(b *AdminFlattenBasis) { b.SourceComplete = false },
		func(b *AdminFlattenBasis) { b.OpenObligations = 1 },
		func(b *AdminFlattenBasis) { b.OpenObligations = -1 },
		func(b *AdminFlattenBasis) { b.PendingSubmissions = 1 },
		func(b *AdminFlattenBasis) { b.PendingSubmissions = -1 },
		func(b *AdminFlattenBasis) { b.CoreFrozen = decimal.RequireFromString("0.000000000000000001") },
		func(b *AdminFlattenBasis) { b.CoreFrozen = decimal.NewFromInt(-1) },
		func(b *AdminFlattenBasis) { b.CoreAvailable = decimal.New(1, 1000) },
		func(b *AdminFlattenBasis) { b.CoreAvailable = b.CoreAvailable.Add(decimal.New(1, -18)) },
		func(b *AdminFlattenBasis) { b.PortfolioQuantity = decimal.Zero },
		func(b *AdminFlattenBasis) { b.Cancellation.State = AdminEmergencyCancelAccepted },
		func(b *AdminFlattenBasis) { b.Cancellation.State = AdminEmergencyCancelFailed },
		func(b *AdminFlattenBasis) { b.Cancellation.EnrollmentComplete = false },
		func(b *AdminFlattenBasis) { b.Cancellation.CompletedAt = time.Time{} },
		func(b *AdminFlattenBasis) {
			b.Cancellation.CompletedAt = b.Cancellation.Acceptance.CreatedAt.Add(-time.Millisecond)
		},
		func(b *AdminFlattenBasis) { b.Cancellation.CompletedAt = b.ObservedAt.Add(time.Millisecond) },
		func(b *AdminFlattenBasis) { b.Cancellation.Revision = 0 },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.ID++ },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.ActorUserID = 0 },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.StrategyID++ },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.Scope = "account" },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.RiskRevision-- },
		func(b *AdminFlattenBasis) { b.Cancellation.Acceptance.ExpectedRiskRevision-- },
	} {
		basis := valid
		mutate(&basis)
		plan, err := PlanAdminFlatten(request, basis)
		if !errors.Is(err, ErrAdminFlattenDenied) || plan.Kind != "" || !plan.Quantity.IsZero() || !plan.Notional.IsZero() {
			t.Fatal("unsafe flatten basis produced even a partial sale plan", err)
		}
	}
}

func TestAdminFlattenNeverRoundsAwayDustOrClipsConfirmedInventory(t *testing.T) {
	t.Parallel()
	request, valid := flattenDomainFixture()
	for _, mutate := range []func(*AdminFlattenBasis){
		func(b *AdminFlattenBasis) { b.LotSize = decimal.RequireFromString("0.001") },
		func(b *AdminFlattenBasis) { b.TickSize = decimal.NewFromInt(3) },
		func(b *AdminFlattenBasis) { b.MinQuantity = decimal.NewFromInt(2) },
		func(b *AdminFlattenBasis) { b.MinNotional = decimal.NewFromInt(124) },
		func(b *AdminFlattenBasis) { b.MaxOrderNotional = decimal.NewFromInt(123) },
		func(b *AdminFlattenBasis) { b.LotSize = decimal.Zero },
		func(b *AdminFlattenBasis) { b.TickSize = decimal.New(1, -19) },
		func(b *AdminFlattenBasis) { b.MaxOrderNotional = decimal.New(1, 1000) },
	} {
		basis := valid
		mutate(&basis)
		if _, err := PlanAdminFlatten(request, basis); !errors.Is(err, ErrAdminFlattenDenied) {
			t.Fatal("flatten silently rounded, clipped or bypassed asset limits", err)
		}
	}
}

func TestAdminFlattenKeepsLedgerDustVisibleAndHonorsExactBoundaries(t *testing.T) {
	t.Parallel()
	request, basis := flattenDomainFixture()
	notional := request.ConfirmedQuantity.Mul(request.LimitPrice)
	basis.MinQuantity = request.ConfirmedQuantity
	basis.MinNotional, basis.MaxOrderNotional = notional, notional
	if _, err := PlanAdminFlatten(request, basis); err != nil {
		t.Fatal("exact quantity and notional boundaries were rejected", err)
	}
	request.ConfirmedQuantity = decimal.RequireFromString("1.2345678912345678911")
	basis.CoreAvailable, basis.PortfolioQuantity = request.ConfirmedQuantity, request.ConfirmedQuantity
	if _, err := request.Normalize(); err != nil {
		t.Fatal("ledger precision was incorrectly hidden at confirmation", err)
	}
	if plan, err := PlanAdminFlatten(request, basis); !errors.Is(err, ErrAdminFlattenDenied) || !plan.Quantity.IsZero() {
		t.Fatal("non-orderable ledger dust was clipped into a sale", err)
	}
}

func TestAdminFlattenRescalesOnlyInsignificantLedgerZeros(t *testing.T) {
	t.Parallel()
	request, basis := flattenDomainFixture()
	exact := request.ConfirmedQuantity
	request.ConfirmedQuantity = decimal.RequireFromString("1.234567891234567891000000000000000000")
	basis.CoreAvailable, basis.PortfolioQuantity = request.ConfirmedQuantity, request.ConfirmedQuantity
	plan, err := PlanAdminFlatten(request, basis)
	if err != nil || !plan.Quantity.Equal(exact) || !FixedDecimal(plan.Quantity, 20, 18, false) {
		t.Fatal("exact ledger zeros were mistaken for unorderable dust", err)
	}
}

func TestAdminFlattenZeroBalanceStillNeedsCurrentSafeProofAndIsNotCompletion(t *testing.T) {
	t.Parallel()
	request, basis := flattenDomainFixture()
	request.ConfirmedQuantity = decimal.Zero
	basis.CoreAvailable, basis.PortfolioQuantity = decimal.Zero, decimal.Zero
	plan, err := PlanAdminFlatten(request, basis)
	if err != nil || plan.Kind != AdminFlattenNoOrder || plan.Side != SideUnspecified || !plan.Quantity.IsZero() || !plan.Notional.IsZero() || plan.ReduceOnly {
		t.Fatal("empty balance manufactured an order or execution result", err)
	}
	basis.PendingSubmissions = 1
	if _, err := PlanAdminFlatten(request, basis); !errors.Is(err, ErrAdminFlattenDenied) {
		t.Fatal("temporary zero balance bypassed an unknown outgoing order", err)
	}
}

func flattenDomainFixture() (AdminFlattenRequest, AdminFlattenBasis) {
	request := AdminFlattenRequest{
		Key:                       "flatten-confirmed-0001",
		Scope:                     AdminFlattenScopeAccountInstrument,
		StrategyID:                9007199254740993,
		AccountID:                 "isolated-simulation-account",
		InstrumentID:              9007199254740995,
		CancellationID:            9007199254740997,
		ExpectedRiskRevision:      9007199254740999,
		ExpectedPortfolioRevision: 9007199254741001,
		ExpectedBaseRevision:      9007199254741003,
		ConfirmedQuantity:         decimal.RequireFromString("1.234567891234567891"),
		LimitPrice:                decimal.RequireFromString("100.01"),
		Reason:                    "explicitly flatten this isolated simulated account",
	}
	basis := AdminFlattenBasis{
		Role:                 AdminRoleAdmin,
		ObservedAt:           time.UnixMilli(3000).UTC(),
		StrategyID:           request.StrategyID,
		AccountID:            request.AccountID,
		InstrumentID:         request.InstrumentID,
		ExecutionVenue:       ExecutionVenueSimulator,
		StrategyCount:        1,
		RiskRevision:         request.ExpectedRiskRevision,
		PortfolioRevision:    request.ExpectedPortfolioRevision,
		BaseRevision:         request.ExpectedBaseRevision,
		ProjectionReconciled: true,
		SourceComplete:       true,
		PortfolioQuantity:    request.ConfirmedQuantity,
		CoreAvailable:        request.ConfirmedQuantity,
		TickSize:             decimal.RequireFromString("0.01"),
		LotSize:              decimal.RequireFromString("0.000000000000000001"),
		MinQuantity:          decimal.RequireFromString("0.001"),
		MinNotional:          decimal.NewFromInt(1),
		MaxOrderNotional:     decimal.NewFromInt(1000),
		Cancellation: AdminEmergencyCancelRecord{
			Acceptance: AdminEmergencyCancelAcceptance{
				ID:                   request.CancellationID,
				ActorUserID:          9007199254740991,
				StrategyID:           request.StrategyID,
				Scope:                AdminEmergencyCancelScopeStrategy,
				ExpectedRiskRevision: request.ExpectedRiskRevision - 1,
				RiskRevision:         request.ExpectedRiskRevision,
				CreatedAt:            time.UnixMilli(1000).UTC(),
			},
			State:              AdminEmergencyCancelSucceeded,
			Revision:           3,
			EnrollmentComplete: true,
			CompletedAt:        time.UnixMilli(2000).UTC(),
		},
	}
	return request, basis
}
