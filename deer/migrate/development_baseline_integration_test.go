//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestDevelopmentBaselineKeepsUnknownAuditAndRiskAndIsOneTime(t *testing.T) {
	db, store, lease, request, snapshot := developmentBaselineFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	preview, err := store.AssessDevelopmentBaseline(ctx, request, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(preview.LegacyNotional).Equal(decimal.NewFromInt(100)) {
		t.Fatal("preview did not retain the whole legacy order risk")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaDevelopmentBaseline{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("assessment mutated audit: count=%d err=%v", count, err)
	}
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatalf("exact approval replay was not idempotent: %v", err)
	}
	changed := proto.CloneOf(request)
	changed.BaselineId += "-other"
	if err := store.ApplyDevelopmentBaseline(ctx, lease, changed, snapshot); err == nil {
		t.Fatal("second baseline on the same account was accepted")
	}
	var old executionmodelv1.HimalayaCancellation
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaCancellation{}).First(&old).Error; err != nil || old.State != himalaya.IntentDispatched {
		t.Fatalf("old unknown audit was rewritten: state=%s err=%v", old.State, err)
	}
	if pending, err := store.PendingIDs(ctx, lease.Key.AccountID); err != nil || len(pending) != 0 {
		t.Fatalf("approved pending IDs=%v err=%v, want no lookup of removed legacy history", pending, err)
	}
	if err := store.SyncBudget(ctx, lease, snapshot, decimal.NewFromInt(950)); err != nil {
		t.Fatal(err)
	}
	budget, err := store.Budget(ctx, lease.Key.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	observed, _ := domain.ParseLedgerDecimal(budget.ObservedNotional, false, true)
	maximum, _ := domain.ParseLedgerDecimal(budget.MaximumNotional, false, false)
	reserved, _ := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if !observed.Equal(decimal.NewFromInt(1050)) || !maximum.Equal(decimal.NewFromInt(1000)) || !reserved.IsZero() || !budget.Enabled {
		t.Fatal("baseline risk was released or financial policy changed")
	}
}

func TestDevelopmentBaselineNeverExcludesNewUnknownCancellations(t *testing.T) {
	_, store, lease, request, snapshot := developmentBaselineFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatal(err)
	}
	scope := himalaya.Scope{
		UserID: request.Scope.UserId,
		FundID: request.Scope.FundId,
		Symbol: request.Scope.Symbol,
	}
	admission, err := store.PrepareCancel(ctx, lease, himalaya.CancelRequest{
		Scope:   scope,
		OrderID: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithCancellation(ctx, admission, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	pending, err := store.PendingIDs(ctx, lease.Key.AccountID)
	if err != nil || len(pending) != 1 || pending[0] != 99 {
		t.Fatalf("PendingIDs(new unknown)=%v,%v, want [99]", pending, err)
	}
	prepared, err := store.Prepare(ctx, lease, developmentSubmit(lease, scope))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); !errors.Is(err, domain.ErrOutcomeUnknown) || calls != 0 {
		t.Fatalf("new unknown cancel was bypassed: calls=%d err=%v", calls, err)
	}
}

func TestDevelopmentBaselineAllowsFreshIntentWithinRetainedBudget(t *testing.T) {
	_, store, lease, request, snapshot := developmentBaselineFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(ctx, lease, developmentSubmit(lease, himalaya.Scope{
		UserID: request.Scope.UserId,
		FundID: request.Scope.FundId,
		Symbol: request.Scope.Symbol,
	}))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("approved baseline fresh intent: calls=%d err=%v", calls, err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); !errors.Is(err, domain.ErrOutcomeUnknown) || calls != 1 {
		t.Fatalf("new unknown intent was replayed: calls=%d err=%v", calls, err)
	}
}

func TestDevelopmentBaselineRejectsUnsafeOrStaleApproval(t *testing.T) {
	for _, kind := range []string{"source_age", "active_order", "foreign_prefix", "unknown_submit", "stale_owner"} {
		t.Run(kind, func(t *testing.T) {
			db, store, lease, request, snapshot := developmentBaselineFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			switch kind {
			case "source_age":
				snapshot.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "active_order":
				snapshot.Orders = proto.CloneOf(request).OriginalOrders
			case "foreign_prefix":
				request.OriginalOrders[0].ClientId = native.ClientPrefix + request.Scope.Symbol + "_foreign"
			case "unknown_submit":
				prepared, err := store.Prepare(ctx, lease, developmentSubmit(lease, himalaya.Scope{
					UserID: request.Scope.UserId,
					FundID: request.Scope.FundId,
					Symbol: request.Scope.Symbol,
				}))
				if err != nil {
					t.Fatal(err)
				}
				if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).UpdateColumn("state", himalaya.IntentDispatched).Error; err != nil {
					t.Fatal(err)
				}
			case "stale_owner":
				if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("epoch", lease.Epoch+1).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err == nil {
				t.Fatal("unsafe development baseline was applied")
			}
			var count int64
			if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaDevelopmentBaseline{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed approval left audit: count=%d err=%v", count, err)
			}
		})
	}
}

func TestDevelopmentBaselineFailsClosedIfRetainedRiskIsReduced(t *testing.T) {
	db, store, lease, request, snapshot := developmentBaselineFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("observed_notional", "899").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.PendingIDs(ctx, lease.Key.AccountID); err == nil {
		t.Fatal("reduced retained risk silently released the historical block")
	}
}

func developmentBaselineFixture(t *testing.T) (*gorm.DB, *himalaya.Store, domain.LeaseToken, *commonv1.DevelopmentBaselineRequest, *commonv1.HimalayaSnapshot) {
	t.Helper()
	db, store, lease, order := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	scope := himalaya.Scope{
		UserID: order.UserID,
		FundID: order.FundID,
		Symbol: order.Symbol,
	}
	admission, err := store.PrepareCancel(ctx, lease, himalaya.CancelRequest{
		Scope:   scope,
		OrderID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithCancellation(ctx, admission, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	wireScope := &commonv1.HimalayaScope{
		UserId: scope.UserID,
		FundId: scope.FundID,
		Symbol: scope.Symbol,
	}
	request := &commonv1.DevelopmentBaselineRequest{
		BaselineId:        "development-test-1",
		ApprovalReference: "test-explicit-development-approval",
		Scope:             wireScope,
		OriginalOrders: []*commonv1.HimalayaOrder{{
			OrderId:          "42",
			ClientId:         native.LegacyPrefix + scope.Symbol + "_old",
			Side:             string(domain.SideBuy),
			Price:            "100",
			Quantity:         "1",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		}},
	}
	snapshot := &commonv1.HimalayaSnapshot{
		Scope: proto.CloneOf(wireScope),
		Market: &commonv1.HimalayaMarket{
			MarkPrice:    "100",
			WorstFeeRate: "0.001",
		},
		Available: "1000",
		Positions: []*commonv1.HimalayaPosition{{
			Symbol:         scope.Symbol,
			SignedQuantity: "8",
			EntryPrice:     "100",
			Leverage:       "10",
		}},
		StartedAtMs:  time.Now().UnixMilli(),
		ObservedAtMs: time.Now().UnixMilli(),
		ValidUntilMs: time.Now().Add(native.SnapshotAge).UnixMilli(),
	}
	return db, store, lease, request, snapshot
}

func developmentSubmit(lease domain.LeaseToken, scope himalaya.Scope) himalaya.SubmitRequest {
	return himalaya.SubmitRequest{
		OwnerID:  lease.OwnerID,
		Epoch:    uint64(lease.Epoch),
		UserID:   scope.UserID,
		FundID:   scope.FundID,
		Symbol:   scope.Symbol,
		ClientID: "development-baseline-test-new-intent",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
		Leverage: 10,
	}
}
