//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func databaseProfileFixture() *commonv1.HimalayaProfilePolicy {
	return &commonv1.HimalayaProfilePolicy{
		UserId:               10001,
		FundId:               10001,
		Symbol:               "BTCUSDT",
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		Leverage:             10,
		MarketDataSource:     native.Source,
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
	}
}

func TestHimalayaProfileBatchIsAtomicAndDoesNotResetOrEnableFunds(t *testing.T) {
	db, store, lease, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	first := databaseProfileFixture()
	second := proto.CloneOf(first)
	second.UserId, second.FundId, second.Symbol = 10002, 10002, "ETHUSDT"
	batch := &commonv1.HimalayaProfileImport{Profiles: []*commonv1.HimalayaProfilePolicy{first, second}}
	if err := store.ImportProfiles(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var rows []*modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Order("user_id").Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatalf("import rows=%d err=%v", len(rows), err)
	}
	oldRisk, e1 := domain.ParseLedgerDecimal(rows[0].ObservedNotional, false, true)
	newRisk, e2 := domain.ParseLedgerDecimal(rows[1].ObservedNotional, false, true)
	if !rows[0].Enabled || rows[1].Enabled || e1 != nil || e2 != nil || !oldRisk.IsPositive() || !newRisk.IsZero() {
		t.Fatal("profile import reset or enabled financial resources")
	}
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("id = ?", rows[1].Id).UpdateColumn("observed_notional", "900").Error; err != nil {
		t.Fatal(err)
	}
	first.PriceTick = "0.2"
	second.MaximumNotional = "100"
	if err := store.ImportProfiles(ctx, batch); !errors.Is(err, himalaya.ErrRiskLimit) {
		t.Fatalf("profile batch reduced limit below retained risk: %v", err)
	}
	var retained modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("id = ?", rows[0].Id).First(&retained).Error; err != nil {
		t.Fatal(err)
	}
	tick, err := domain.ParseLedgerDecimal(retained.PriceTick, false, false)
	if err != nil || tick.String() != "0.1" {
		t.Fatalf("failed second row committed first profile: tick=%s err=%v", retained.PriceTick, err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.MarketKey(first.Symbol),
	}, lease.OwnerID, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportProfiles(ctx, &commonv1.HimalayaProfileImport{Profiles: []*commonv1.HimalayaProfilePolicy{first}}); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("profile import changed a live source binding: %v", err)
	}
}

func TestHimalayaProfileMaintenanceRejectsActiveOwnerAndRetainsRisk(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var before modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	profile := &commonv1.HimalayaProfilePolicy{
		UserId:               request.UserID,
		FundId:               request.FundID,
		Symbol:               request.Symbol,
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		Leverage:             10,
		MarketDataSource:     native.Source,
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
	}
	if err := store.InitializeBudget(ctx, profile, true); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("business policy changed under an active execution owner: %v", err)
	}
	var after modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&after).Error; err != nil || !proto.Equal(&before, &after) {
		t.Fatalf("rejected maintenance mutated retained budget: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBudget(ctx, profile, true); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.ObservedNotional != before.ObservedNotional || after.ReservedNotional != before.ReservedNotional || after.AvailableQuote != before.AvailableQuote || after.SnapshotExpiresAt != before.SnapshotExpiresAt || after.Id != before.Id || after.CreateAt != before.CreateAt || after.Symbol != request.Symbol {
		t.Fatal("maintenance reset financial observations, identity or risk")
	}
}

func TestHimalayaRuntimeLoadsDatabasePolicyWithoutStartupWrites(t *testing.T) {
	db, _, _, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cfg := &configv1.BusinessRuntime{
		PollInterval:     durationpb.New(100 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Himalaya: &configv1.HimalayaRuntime{
			QuantityFraction: "0.1",
			Depth:            20,
			LeaseTtl:         durationpb.New(10 * time.Second),
		},
	}
	if _, err := native.LoadRuntime(ctx, db, cfg); !errors.Is(err, native.ErrInvalid) {
		t.Fatalf("unconfigured legacy budget silently supplied a profile: %v", err)
	}
	configureNativeProfileFixture(ctx, t, db, request)
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("user_id = ?", request.UserID).Select("MaximumOrderNotional", "Leverage").Updates(&modelv1.HimalayaBudget{
		MaximumOrderNotional: "100",
		Leverage:             10,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var before modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := native.LoadRuntime(ctx, db, cfg)
	if err != nil || len(settings.Himalaya.Profiles) != 1 || settings.Himalaya.Profiles[0].MaximumNotional != "1000" || settings.Himalaya.Profiles[0].PriceTick != "0.1" {
		t.Fatalf("database profile load: %v, %v", settings, err)
	}
	var after modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&after).Error; err != nil || !proto.Equal(&before, &after) {
		t.Fatalf("startup wrote budget data: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("id = ?", before.Id).UpdateColumn("price_tick", "0.2").Error; err != nil {
		t.Fatal(err)
	}
	if settings.Himalaya.Profiles[0].PriceTick != "0.1" {
		t.Fatal("database update silently hot-switched an active startup snapshot")
	}
	restarted, err := native.LoadRuntime(ctx, db, cfg)
	if err != nil || restarted.Himalaya.Profiles[0].PriceTick != "0.2" {
		t.Fatalf("explicit restart did not reload database policy: %v", err)
	}
}
