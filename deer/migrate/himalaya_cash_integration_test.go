//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestHimalayaLiveMarginAndExpiredUnsentIntentRemainSafe(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	authority := liveAuthorityFixture(ctx, t, db, lease, request.Symbol)
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("available_quote", "1").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareLive(ctx, lease, request, authority); !errors.Is(err, himalaya.ErrRiskLimit) {
		t.Fatalf("margin shortage accepted: %v", err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("available_quote", "1000").Error; err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareLive(ctx, lease, request, authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).UpdateColumn("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, lease, &commonv1.HimalayaSnapshot{Scope: &commonv1.HimalayaScope{
		UserId: request.UserID,
		FundId: request.FundID,
		Symbol: request.Symbol,
	}}); err != nil {
		t.Fatal(err)
	}
	request.ClientID += "-fresh"
	authority.Book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	if _, err := store.PrepareLive(ctx, lease, request, authority); err != nil {
		t.Fatalf("proven unsent reservation did not expire atomically: %v", err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { t.Error("expired prepared intent escaped"); return nil }); err == nil {
		t.Fatal("expired intent was admitted")
	}
}
