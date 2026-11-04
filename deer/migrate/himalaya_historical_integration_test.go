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

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestHimalayaHistoricalApprovalPreservesUnknownRiskAndNewUnknownGate(t *testing.T) {
	db, store, lease, order := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).
		UpdateColumn("maximum_notional", "1500").Error; err != nil {
		t.Fatal(err)
	}
	old, err := store.Prepare(ctx, lease, order)
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("test lost reply")
	if err := store.WithSubmission(ctx, old.Admission, func(context.Context) error { return lost }); !errors.Is(err, lost) {
		t.Fatal(err)
	}
	snapshot := &commonv1.HimalayaSnapshot{
		Scope: &commonv1.HimalayaScope{
			UserId: order.UserID,
			FundId: order.FundID,
			Symbol: order.Symbol,
		},
		StartedAtMs:  time.Now().UnixMilli(),
		ObservedAtMs: time.Now().UnixMilli(),
		ValidUntilMs: time.Now().Add(native.SnapshotAge).UnixMilli(),
	}
	approval := &commonv1.HistoricalIntentApproval{
		ApprovalReference: "explicit-development-restart-test",
		Scope:             proto.CloneOf(snapshot.Scope),
		IntentIds:         []int64{old.Request.IntentID},
	}
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", old.Request.IntentID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("approval snapshot start=%d observed=%d validUntil=%d databaseNow=%d", snapshot.StartedAtMs, snapshot.ObservedAtMs, snapshot.ValidUntilMs, clock.NowMS)
	if err := store.AssessHistoricalIntents(ctx, lease, approval, snapshot); err != nil {
		t.Fatal(err)
	}
	var preview executionmodelv1.HimalayaIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", old.Request.IntentID).First(&preview).Error; err != nil || preview.HistoricalApprovalReference != "" {
		t.Fatalf("AssessHistoricalIntents mutated approval: %q, %v", preview.HistoricalApprovalReference, err)
	}
	for range 2 {
		if err := store.ApproveHistoricalIntents(ctx, lease, approval, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	var row executionmodelv1.HimalayaIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", old.Request.IntentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != himalaya.IntentDispatched || row.OrderId != "" || !proto.Equal(row.HistoricalSnapshot, snapshot) {
		t.Fatalf("ApproveHistoricalIntents changed unknown fact or lost evidence: %v", &row)
	}
	order.ClientID += "-new"
	fresh, err := store.Prepare(ctx, lease, order)
	if err != nil {
		t.Fatalf("Prepare(after explicit historical approval) = %v, want fresh admission with old risk retained", err)
	}
	if err := store.WithSubmission(ctx, fresh.Admission, func(context.Context) error { return lost }); !errors.Is(err, lost) {
		t.Fatal(err)
	}
	order.ClientID += "-later"
	if _, err := store.Prepare(ctx, lease, order); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("Prepare(new unknown) = %v, want outcome unknown", err)
	}
	calls := 0
	if err := store.WithSubmission(ctx, old.Admission, func(context.Context) error { calls++; return nil }); !errors.Is(err, domain.ErrOutcomeUnknown) || calls != 0 {
		t.Fatalf("WithSubmission(historical request) calls=%d err=%v, want no replay", calls, err)
	}
	budget, err := store.Budget(ctx, lease.Key.AccountID)
	if err != nil || !decimal.RequireFromString(budget.GetReservedNotional()).Equal(decimal.NewFromInt(200)) {
		t.Fatalf("historical/new unknown risk = %s, %v, want 200 retained", budget.GetReservedNotional(), err)
	}
}

func TestHimalayaHistoricalApprovalRejectsUnsafeRequestsAtomically(t *testing.T) {
	for _, changed := range []string{"active_order", "wrong_scope", "expired_view", "future_view", "missing_id", "reduced_reservation", "stale_epoch"} {
		t.Run(changed, func(t *testing.T) {
			db, store, lease, order := sdkAdmissionFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			prepared, err := store.Prepare(ctx, lease, order)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UnixMilli()
			snapshot := &commonv1.HimalayaSnapshot{
				Scope: &commonv1.HimalayaScope{
					UserId: order.UserID,
					FundId: order.FundID,
					Symbol: order.Symbol,
				},
				StartedAtMs:  now,
				ObservedAtMs: now,
				ValidUntilMs: now + native.SnapshotAge.Milliseconds(),
			}
			approval := &commonv1.HistoricalIntentApproval{
				ApprovalReference: "explicit-invalid-approval-test",
				Scope:             proto.CloneOf(snapshot.Scope),
				IntentIds:         []int64{prepared.Request.IntentID},
			}
			switch changed {
			case "active_order":
				snapshot.Orders = []*commonv1.HimalayaOrder{{OrderId: "42"}}
			case "wrong_scope":
				approval.Scope.FundId++
			case "expired_view":
				snapshot.ValidUntilMs = now - 1
			case "future_view":
				snapshot.ObservedAtMs = now + 5000
			case "missing_id":
				approval.IntentIds = append(approval.IntentIds, prepared.Request.IntentID+1)
			case "reduced_reservation":
				err = db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("reserved_notional", "0").Error
			case "stale_epoch":
				lease.Epoch++
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ApproveHistoricalIntents(ctx, lease, approval, snapshot); err == nil {
				t.Fatalf("ApproveHistoricalIntents(%s) = nil, want rejection", changed)
			}
			var row executionmodelv1.HimalayaIntent
			if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil || row.HistoricalApprovalReference != "" || row.State != himalaya.IntentDispatched {
				t.Fatalf("failed approval left a partial mutation: %v, %v", &row, err)
			}
		})
	}
}

func TestHimalayaUnsentFinalGuardFailureDoesNotCreatePermanentUnknown(t *testing.T) {
	db, store, lease, order := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, order)
	if err != nil {
		t.Fatal(err)
	}
	const hook = "test:change-epoch-after-durable-claim"
	injected := false
	if err := db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*executionmodelv1.HimalayaIntent); !ok || injected {
			return
		}
		injected = true
		if err := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&commonmodelv1.ExecutionShardLease{}).
			Where("account_id = ?", lease.Key.AccountID).UpdateColumn("epoch", lease.Epoch+1).Error; err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(hook); err != nil {
			t.Error(err)
		}
	})
	calls := 0
	err = store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, domain.ErrLeaseLost) || calls != 0 || !injected {
		t.Fatalf("WithSubmission(changed epoch) calls=%d err=%v injected=%t, want lease lost before send", calls, err, injected)
	}
	var row executionmodelv1.HimalayaIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	budget, err := store.Budget(ctx, lease.Key.AccountID)
	if err != nil || row.State != himalaya.IntentTerminal || !decimal.RequireFromString(budget.GetReservedNotional()).IsZero() {
		t.Fatalf("unsent final guard failure state=%s reservation=%s err=%v, want terminal and zero", row.State, budget.GetReservedNotional(), err)
	}
}
