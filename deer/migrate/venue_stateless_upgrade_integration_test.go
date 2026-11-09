//go:build integration

package migrate

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

	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestVenueStatelessHistoryUpgradeAppendsWithoutRewritingExistingData(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	before, account := statelessHistoryFixture(ctx, t, db)
	if err := venuemigrate.Check(ctx, db); !errors.Is(err, venuemigrate.ErrHistory) {
		t.Fatalf("Check(stateless history before explicit upgrade) = %v, want ErrHistory", err)
	}
	if err := venuemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("Apply(exact stateless history) = %v, want append-only upgrade", err)
	}
	if err := venuemigrate.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	var rows []*commonmodelv1.VenueSchemaMigration
	if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Order("version").Find(&rows).Error; err != nil || len(rows) != 11 {
		t.Fatalf("stateless upgrade history = %d rows, %v, want 11 including unlimited synthetic slots", len(rows), err)
	}
	assertUnlimitedVenueLedger(t, rows[10])

	for i, old := range before {
		if !proto.Equal(old, rows[i]) {
			t.Errorf("stateless upgrade changed historical migration %d", old.Version)
		}
	}
	var budget commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", account).Take(&budget).Error; err != nil {
		t.Fatal(err)
	}
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, false)
	if err != nil || reserved.String() != "50" || budget.Id != 41 || budget.CreateAt != 101 || budget.UpdateAt != 102 || !budget.Enabled || !budget.CutoverCompleted {
		t.Fatalf("stateless upgrade changed retained financial policy: %v, %v", &budget, err)
	}
	var grace struct{ Value int64 }
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Select("plan_grace_ms AS value").Where("account_id = ?", account).Take(&grace).Error; err != nil || grace.Value != 0 {
		t.Fatalf("stateless upgrade changed historical grace value: %d, %v", grace.Value, err)
	}
	var snapshot portfoliomodelv1.VenueAccountSnapshot
	if err := db.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Where("account_id = ?", account).Take(&snapshot).Error; err != nil || snapshot.Id != 42 || snapshot.CreateAt != 103 || snapshot.UpdateAt != 104 || snapshot.PublisherEpoch != 7 || snapshot.Snapshot.GetObservedAtMs() != 100 {
		t.Fatalf("stateless upgrade changed retained projection: %v, %v", &snapshot, err)
	}
	for _, entity := range []any{&executionmodelv1.VenueOrderIntent{}, &executionmodelv1.VenueCancellation{}, &executionmodelv1.SyntheticTapeIntent{}, &executionmodelv1.ExecutionRecoveryBaseline{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("future audit schema %T = %d rows, %v, want empty without fabricated history", entity, count, err)
		}
	}
	if err := venuemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("repeated stateless upgrade = %v, want no change", err)
	}
}

func statelessHistoryFixture(ctx context.Context, t *testing.T, db *gorm.DB) ([]*commonmodelv1.VenueSchemaMigration, string) {
	t.Helper()
	paths := []string{
		"venue/sql/0001_sdk_intents.sql", "venue/sql/0002_live_execution.sql",
		"venue/sql/0003_synthetic_tape.sql", "venue/sql/0004_development_baseline.sql",
		"venue/sql/0005_historical_intent_approval.sql", "venue/sql/0006_database_profiles.sql",
		"venue/legacy/0007_stateless_orders.sql", "venue/legacy/0008_plan_grace.sql",
	}
	before := make([]*commonmodelv1.VenueSchemaMigration, 0, len(paths))
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// This fixture reproduces the exact already-applied historical schema.
		if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec(string(data)).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec("ALTER TABLE himalaya_schema_migrations RENAME TO venue_schema_migrations").Error; err != nil {
				t.Fatal(err)
			}
		}
		row := &commonmodelv1.VenueSchemaMigration{
			Version:  int64(i + 1),
			Checksum: fmt.Sprintf("%x", sha256.Sum256(data)),
		}
		if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
		before = append(before, proto.CloneOf(row))
	}
	account := native.ScopeKey(101, 201)
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Create(&commonmodelv1.ExecutionShardLease{AccountId: account}).Error; err != nil {
		t.Fatal(err)
	}
	// Literal historical SQL is required because these provider-era tables do
	// not have current production models. No historical record is reconstructed.
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Exec(`INSERT INTO himalaya_budgets
        (id,create_at,update_at,account_id,user_id,fund_id,maximum_notional,observed_notional,reserved_notional,snapshot_expires_at,enabled,
         available_quote,maximum_order_notional,leverage,worst_fee_rate,cutover_completed,symbol,market_data_source,price_tick,quantity_step,plan_grace_ms)
        VALUES (41,101,102,?,101,201,1000,200,50,100,true,750,100,10,0.001,true,'BTCUSDT','binance',0.1,0.001,0)`, account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Exec(`INSERT INTO himalaya_projections
        (id,create_at,update_at,account_id,snapshot,publisher_owner_id,publisher_epoch)
        VALUES (42,103,104,?,'{"observed_at_ms":100,"valid_until_ms":101}'::jsonb,'legacy-publisher',7)`, account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec("ALTER TABLE venue_schema_migrations RENAME TO himalaya_schema_migrations").Error; err != nil {
		t.Fatal(err)
	}
	return before, account
}

func assertUnlimitedVenueLedger(t *testing.T, row *commonmodelv1.VenueSchemaMigration) {
	t.Helper()
	unlimited, err := os.ReadFile("venue/sql/0010_unlimited_synthetic.sql")
	if err != nil {
		t.Fatal(err)
	}
	if row.Version != 11 || row.Checksum != fmt.Sprintf("%x", sha256.Sum256(unlimited)) {
		t.Fatal("unlimited synthetic migration missing or changed")
	}
}
