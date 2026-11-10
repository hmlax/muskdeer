//go:build integration

package instancemove

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	values "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

func baselineDatabase(ctx context.Context, t *testing.T) (*gorm.DB, *common.SchemaMigration) {
	t.Helper()
	db := database(t)
	definition, err := os.ReadFile("../schemaformat/0001_strategy_instances.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&common.SchemaMigration{}).Exec(string(definition)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Create(strategyregistry.Templates()).Error; err != nil {
		t.Fatal(err)
	}
	row := &common.SchemaMigration{
		Version:  1,
		Name:     "0001_strategy_instances.sql",
		Checksum: fmt.Sprintf("%x", sha256.Sum256(definition)),
	}
	if err := db.WithContext(ctx).Model(&common.SchemaMigration{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return db, row
}

func TestUnlimitedSchemaUpgradePreservesBaselineAndRejectsActiveOwners(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, before := baselineDatabase(ctx, t)
	if err := schemaformat.Check(ctx, db); err == nil {
		t.Fatal("Check(old baseline) accepted missing explicit upgrade")
	}
	old := &execution.SyntheticTapeIntent{
		RunId:        "preserved-run",
		Symbol:       "BTCUSDT",
		Sequence:     999,
		PolicyDigest: "unchanged-history",
		Plan:         &values.SyntheticTapePlan{Sequence: 999},
		Notional:     "1",
		Result:       &values.SyntheticTapeResult{State: native.SyntheticUnknown},
	}
	if err := db.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Create(old).Error; err != nil {
		t.Fatal(err)
	}
	var storedBefore execution.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Where("id = ?", old.Id).Take(&storedBefore).Error; err != nil {
		t.Fatal(err)
	}
	old = &storedBefore
	lease := &common.ExecutionShardLease{
		AccountId: "upgrade-owner",
		OwnerId:   "active-owner",
		Epoch:     1,
		ExpiresAt: time.Now().Add(time.Minute).UnixMilli(),
	}
	if err := db.WithContext(ctx).Model(&common.ExecutionShardLease{}).Create(lease).Error; err != nil {
		t.Fatal(err)
	}
	if err := schemaformat.Upgrade(ctx, db); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("Upgrade(active owner) = %v, want lease-held rejection", err)
	}
	if err := db.WithContext(ctx).Model(&common.ExecutionShardLease{}).Where("id = ?", lease.Id).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := schemaformat.Upgrade(ctx, db); err != nil {
			t.Fatalf("Upgrade(offlined baseline) = %v", err)
		}
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	var ledger []*common.SchemaMigration
	if err := db.WithContext(ctx).Model(&common.SchemaMigration{}).Order("version").Find(&ledger).Error; err != nil || len(ledger) != 6 || !proto.Equal(before, ledger[0]) {
		t.Fatalf("Upgrade changed baseline or duplicated history: count=%d err=%v", len(ledger), err)
	}
	if !db.WithContext(ctx).Migrator().HasIndex(&execution.VenueOrderIntent{}, "idx_venue_intent_current_reservation") {
		t.Fatal("current reservation covering index missing after explicit upgrade")
	}
	var preserved execution.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Where("id = ?", old.Id).Take(&preserved).Error; err != nil || !proto.Equal(old, &preserved) {
		t.Fatalf("Upgrade changed old unknown slot: %v", err)
	}
	next := proto.CloneOf(old)
	next.Id, next.Sequence, next.Plan.Sequence = 0, 1000, 1000
	if err := db.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Create(next).Error; err != nil {
		t.Fatalf("upgraded schema rejected sequence 1000: %v", err)
	}
	next.Id, next.Sequence = 0, -1
	if err := db.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Create(next).Error; err == nil {
		t.Fatal("upgraded schema accepted a negative sequence")
	}
}
