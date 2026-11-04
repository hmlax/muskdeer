//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

func TestDevelopmentBaselineRechecksRetainedRiskAfterDispatchClaim(t *testing.T) {
	db, store, lease, request, snapshot := developmentBaselineFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(ctx, lease, developmentSubmit(lease, venueexecution.Scope{
		UserID: request.Scope.UserId,
		FundID: request.Scope.FundId,
		Symbol: request.Scope.Symbol,
	}))
	if err != nil {
		t.Fatal(err)
	}
	const hook = "test:reduce-development-hold-after-claim"
	injected := false
	err = db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*executionmodelv1.VenueOrderIntent); !ok || injected {
			return
		}
		injected = true
		change := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("observed_notional", "899")
		if change.Error != nil {
			t.Error(change.Error)
		}
		t.Logf("injected update rows=%d", change.RowsAffected)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(hook); err != nil {
			t.Error(err)
		}
	})
	calls := 0
	err = store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil })
	t.Logf("final result calls=%d err=%v", calls, err)
	budget, readErr := store.Budget(ctx, lease.Key.AccountID)
	observed, parseErr := domain.ParseLedgerDecimal(budget.GetObservedNotional(), false, true)
	if readErr != nil || parseErr != nil || !observed.Equal(decimal.NewFromInt(899)) {
		t.Fatalf("fault did not change retained risk: observed=%s err=%v", budget.GetObservedNotional(), readErr)
	}
	if !injected || !errors.Is(err, venueexecution.ErrRiskLimit) || calls != 0 {
		t.Fatalf("retained risk changed after claim: injected=%t calls=%d err=%v", injected, calls, err)
	}
}
