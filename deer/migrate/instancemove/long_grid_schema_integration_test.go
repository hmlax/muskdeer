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
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"google.golang.org/protobuf/proto"
)

func TestLongGridUpgradePreservesFiveLedgerRowsAndExistingTemplates(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, _ := baselineDatabase(ctx, t)
	// Reproduce a pre-feature database, rather than installing new models first.
	if err := db.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Where("id = ?", strategyregistry.LongGridTemplateID).Delete(&strategy.StrategyTemplate{}).Error; err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"0002_unlimited_synthetic.sql", "0003_console_instance_identity.sql", "0004_current_reservation_index.sql", "0005_matching_authority.sql"} {
		data, err := os.ReadFile("../schemaformat/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec(string(data)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
			Version:  int64(i + 2),
			Name:     name,
			Checksum: fmt.Sprintf("%x", sha256.Sum256(data)),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before []*model.SchemaMigration
	var templates []*strategy.StrategyTemplate
	if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Order("version").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Order("id").Find(&templates).Error; err != nil {
		t.Fatal(err)
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
		t.Fatalf("ledger length %d", len(after))
	}
	for index, old := range before {
		if !proto.Equal(old, after[index]) {
			t.Fatal("old ledger rewritten")
		}
	}
	var current []*strategy.StrategyTemplate
	if err := db.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Order("id").Find(&current).Error; err != nil {
		t.Fatal(err)
	}
	if len(current) != 4 {
		t.Fatal("new compiled template missing")
	}
	for index, old := range templates {
		if !proto.Equal(old, current[index]) {
			t.Fatal("old template changed")
		}
	}
	for _, entity := range []any{&execution.LongGridRuntime{}, &execution.LongGridAction{}} {
		if !db.WithContext(ctx).Migrator().HasTable(entity) {
			t.Fatal("grid business model table missing")
		}
	}
}
