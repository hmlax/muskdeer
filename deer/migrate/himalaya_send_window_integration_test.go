//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

func TestHimalayaLiveDispatchRequiresEnoughTimeBeforeEnteringSDK(t *testing.T) {
	db, store, lease, order := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	authority := liveAuthorityFixture(ctx, t, db, lease, order.Symbol)
	prepared, err := store.PrepareLive(ctx, lease, order, authority)
	if err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	calls := 0
	err = store.WithSubmission(short, prepared.Admission, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, domain.ErrLeaseLost) || calls != 0 {
		t.Fatalf("WithSubmission(200ms remaining) calls=%d err=%v, want rejection before SDK", calls, err)
	}
	var row executionmodelv1.VenueOrderIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State == venueexecution.IntentDispatched {
		t.Fatalf("near-expiry request became unknown without SDK entry: %v", &row)
	}
}
