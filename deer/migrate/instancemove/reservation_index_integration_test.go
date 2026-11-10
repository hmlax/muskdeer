//go:build integration

package instancemove

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"google.golang.org/protobuf/proto"
)

func TestMatchingAuthorityUpgradeRetainsAllFourHistoricalLedgerRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	db, _ := baselineDatabase(ctx, t)
	for i, name := range []string{"0002_unlimited_synthetic.sql", "0003_console_instance_identity.sql", "0004_current_reservation_index.sql"} {
		definition, err := os.ReadFile("../schemaformat/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec(string(definition)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
			Version:  int64(i + 2),
			Name:     name,
			Checksum: fmt.Sprintf("%x", sha256.Sum256(definition)),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	audit := &model.ExecutionShardLease{
		AccountId: "historical-owner",
		OwnerId:   "original-process",
		Epoch:     7,
	}
	if err := db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Create(audit).Error; err != nil {
		t.Fatal(err)
	}
	var before []*model.SchemaMigration
	if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Order("version").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := schemaformat.Check(ctx, db); err == nil {
		t.Fatal("missing explicit index upgrade accepted")
	}
	for range 2 {
		if err := schemaformat.Upgrade(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after []*model.SchemaMigration
	if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Order("version").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != 6 {
		t.Fatalf("ledger rows=%d, want 6", len(after))
	}
	for i, old := range before {
		if !proto.Equal(old, after[i]) {
			t.Fatalf("historical ledger %d changed", i+1)
		}
	}
	var preserved model.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Where("id = ?", audit.Id).Take(&preserved).Error; err != nil || !proto.Equal(audit, &preserved) {
		t.Fatalf("historical process authority changed during upgrade: %v", err)
	}
	if !db.WithContext(ctx).Migrator().HasIndex(&execution.VenueOrderIntent{}, "idx_venue_intent_current_reservation") {
		t.Fatal("covering index missing")
	}
}
