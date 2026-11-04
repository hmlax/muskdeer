//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestHimalayaLiveSourceAndStrategyEpochAreCheckedAtFinalSDKCall(t *testing.T) {
	for _, changed := range []string{"source", "strategy", "deadline"} {
		t.Run(changed, func(t *testing.T) {
			db, store, lease, request := sdkAdmissionFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			authority := liveAuthorityFixture(ctx, t, db, lease, request.Symbol)
			prepared, err := store.PrepareLive(ctx, lease, request, authority)
			if err != nil {
				t.Fatal(err)
			}
			key := authority.Book.Publisher.AccountId
			if changed == "strategy" {
				key = authority.Strategy.AccountId
			}
			if changed == "deadline" {
				err = db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).UpdateColumn("expires_at", 1).Error
			} else {
				err = db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", key).UpdateColumn("epoch", 2).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil })
			if err == nil || calls != 0 {
				t.Fatalf("stale %s reached SDK: calls=%d err=%v", changed, calls, err)
			}
		})
	}
}

func TestHimalayaCancelReplyDoesNotReleaseExposureBeforeTerminalObservation(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	order := &commonv1.HimalayaOrder{
		OrderId:          "42",
		ClientId:         prepared.Request.ClientID,
		Side:             string(request.Side),
		Price:            request.Price.String(),
		Quantity:         request.Quantity.String(),
		ExecutedQuantity: "0.25",
		Status:           native.StatusPartial,
	}
	if err := store.Observe(ctx, lease, &commonv1.HimalayaSnapshot{
		Scope: &commonv1.HimalayaScope{
			UserId: request.UserID,
			FundId: request.FundID,
			Symbol: request.Symbol,
		},
		Orders: []*commonv1.HimalayaOrder{order},
	}); err != nil {
		t.Fatal(err)
	}
	cancellation, err := store.PrepareCancel(ctx, lease, himalaya.CancelRequest{
		Scope: himalaya.Scope{
			UserID: request.UserID,
			FundID: request.FundID,
			Symbol: request.Symbol,
		},
		OrderID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithCancellation(ctx, cancellation, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var budget commonmodelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).First(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(budget.ReservedNotional).Equal(decimal.NewFromInt(100)) {
		t.Fatal("cancel acknowledgement released pending/filled exposure")
	}
	order.Status = native.StatusPartCancel
	for range 2 {
		if err := store.Observe(ctx, lease, &commonv1.HimalayaSnapshot{
			Scope: &commonv1.HimalayaScope{
				UserId: request.UserID,
				FundId: request.FundID,
				Symbol: request.Symbol,
			},
			Orders: []*commonv1.HimalayaOrder{order},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).First(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(budget.ReservedNotional).Equal(decimal.NewFromInt(25)) {
		t.Fatal("terminal observation released fills or applied the release twice")
	}
	if err := store.WithCancellation(ctx, cancellation, func(context.Context) error { t.Error("cancel was replayed"); return nil }); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("cancel claim was reused: %v", err)
	}
}

func liveAuthorityFixture(ctx context.Context, t *testing.T, db *gorm.DB, execution domain.LeaseToken, symbol string) himalaya.LiveAuthority {
	t.Helper()
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	get := func(key string) *commonv1.HimalayaAuthority {
		token, err := leases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: key,
		}, execution.OwnerID, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return &commonv1.HimalayaAuthority{
			AccountId:   key,
			OwnerId:     token.OwnerID,
			Epoch:       token.Epoch,
			ExpiresAtMs: token.ExpiresAt.UnixMilli(),
		}
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", execution.Key.AccountID).
		Updates(&commonmodelv1.HimalayaBudget{
			AvailableQuote:       "1000",
			MaximumOrderNotional: "1000",
			Leverage:             10,
			WorstFeeRate:         "0.001",
			CutoverCompleted:     true,
		}).Error; err != nil {
		t.Fatal(err)
	}
	return himalaya.LiveAuthority{
		Strategy: get(native.StrategyKey(10001, 10001)),
		Book: &commonv1.HimalayaBook{
			Symbol:       symbol,
			Source:       native.Source,
			Segment:      native.Segment,
			ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
			Publisher:    get(native.MarketKey(symbol)),
		},
	}
}
