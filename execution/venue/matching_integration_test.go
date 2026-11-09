//go:build integration

package venue

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/adapters/himalaya"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestMatchingAdmissionWithoutProcessLeaseOrHistoryReservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	largeUnknown := proto.CloneOf(setup.old)
	largeUnknown.Id = 0
	largeUnknown.ClientId = "large-old-unknown"
	largeUnknown.Price, largeUnknown.ReservedNotional = "100000", "100000"
	if err := setup.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Create(largeUnknown).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewMatchingStore(setup.db, himalaya.Factory{})
	if err != nil {
		t.Fatal(err)
	}
	setup.runtime.store = store
	forbidden := denyMatchingQueries(t, setup.db)
	if _, err := setup.runtime.refresh(ctx, setup.profile, domain.LeaseToken{}, nil); err != nil {
		t.Fatalf("refresh(no process lease) = %v", err)
	}
	now := time.Now().UnixMilli()
	live := LiveAuthority{
		InstanceID: setup.instance.Id,
		Revision:   setup.instance.Revision,
		Strategy:   &common.VenueAuthority{AccountId: native.StrategyKey(10001, 10001)},
		Book: &common.VenueBook{
			Source:       native.Source,
			Segment:      native.Segment,
			Symbol:       setup.profile.Symbol,
			ValidUntilMs: now + native.BookAge.Milliseconds(),
			Publisher:    &common.VenueAuthority{AccountId: native.MarketKey(setup.profile.Symbol)},
		},
	}
	request := SubmitRequest{
		UserID:   10001,
		FundID:   10001,
		Symbol:   setup.profile.Symbol,
		ClientID: "matching-no-reservation",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
		Leverage: 10,
	}
	prepared, err := store.PrepareLive(ctx, domain.LeaseToken{}, request, live)
	if err != nil {
		t.Fatalf("PrepareLive(large historical reserve, no process lease) = %v", err)
	}
	var calls atomic.Int64
	send := func(context.Context) error { calls.Add(1); return nil }
	if err := store.WithSubmission(ctx, prepared.Admission, send); err != nil {
		t.Fatalf("WithSubmission(no process lease) = %v", err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, send); !errors.Is(err, domain.ErrOutcomeUnknown) || calls.Load() != 1 {
		t.Fatalf("repeat send = %v, calls %d; want unknown and one send", err, calls.Load())
	}
	cancellation, err := store.PrepareCancel(ctx, domain.LeaseToken{}, CancelRequest{
		Scope: Scope{
			UserID: request.UserID,
			FundID: request.FundID,
			Symbol: request.Symbol,
		},
		OrderID: 77,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithCancellation(ctx, cancellation, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("WithCancellation(no process lease) = %v", err)
	}
	if err := store.Acknowledge(ctx, prepared.Admission, &gateway.SubmitResult{
		OrderID:  77,
		ClientID: request.ClientID,
	}); err != nil {
		t.Fatal(err)
	}
	var row execution.VenueOrderIntent
	if err := setup.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", prepared.Admission.IntentID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	budget, err := store.Budget(ctx, setup.budget.AccountId)
	if err != nil || row.OwnerId != "" || row.OwnerEpoch != 0 || !decimal.RequireFromString(row.ReservedNotional).IsZero() ||
		!decimal.RequireFromString(budget.ReservedNotional).Equal(decimal.RequireFromString(setup.budget.ReservedNotional)) {
		t.Fatalf("new intent has process ownership/reservation, or historical reserve changed: %v", err)
	}
	// User controls still win at the final send, independently of process identity.
	request.ClientID += "-paused"
	prepared, err = store.PrepareLive(ctx, domain.LeaseToken{}, request, live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStop,
		Subject:                "matching-test",
		RequestID:              "matching-test-stop-current",
		ID:                     setup.instance.Id,
		ExpectedRevision:       setup.instance.Revision,
		ExpectedControlVersion: setup.instance.RequestedConfigVersion,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, send); err == nil || calls.Load() != 1 {
		t.Fatalf("send after stop = %v, calls %d; want rejection", err, calls.Load())
	}
	var old execution.VenueOrderIntent
	if err := setup.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", setup.old.Id).Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	if old.State != IntentDispatched || !decimal.RequireFromString(old.ReservedNotional).Equal(decimal.RequireFromString(setup.old.ReservedNotional)) || old.OwnerId != setup.old.OwnerId {
		t.Fatal("historical unknown identity or reservation changed")
	}
	if forbidden.Load() != 0 {
		t.Fatalf("process lease accesses = %d", forbidden.Load())
	}
}

func denyMatchingQueries(t *testing.T, db *gorm.DB) *atomic.Int64 {
	t.Helper()
	var forbidden atomic.Int64
	// Fail queries and writes to process leases, including final egress paths.
	deny := func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "execution_shard_leases" {
			forbidden.Add(1)
			tx.Error = errors.Join(tx.Error, errors.New("process lease accessed"))
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("matching_no_lease", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("matching_no_lease", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("matching_no_lease", deny); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("matching_no_reservation_sum", func(tx *gorm.DB) {
		query := strings.ToLower(tx.Statement.SQL.String())
		if strings.Contains(query, "sum(") && strings.Contains(query, "reserved_notional") {
			forbidden.Add(1)
			tx.Error = errors.Join(tx.Error, errors.New("reservation history summed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	return &forbidden
}
