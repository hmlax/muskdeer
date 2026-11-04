//go:build integration

package migrate

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
)

// Reviewed legacy SQL fixtures exercise the old schema without retaining old ORM types.
func seedVenueUpgradeRecords(ctx context.Context, t *testing.T, db *gorm.DB, account string) func() {
	t.Helper()
	var checks []func()
	fixtures := []struct {
		model     proto.Message
		statement string
	}{
		{
			model: &executionmodelv1.VenueOrderIntent{},
			statement: `INSERT INTO himalaya_intents
                (create_at, update_at, account_id, user_id, fund_id, symbol, client_id,
                 owner_id, owner_epoch, request_digest, side, price, quantity, leverage,
                 state, order_id, reserved_notional)
                VALUES (201,202,?,101,201,'BTCUSDT','legacy-unknown','legacy-owner',3,
                        'legacy-digest','BUY',100,0.5,10,'DISPATCHED','',50) RETURNING *`,
		},
		{
			model: &executionmodelv1.VenueCancellation{},
			statement: `INSERT INTO himalaya_cancellations
                (create_at, update_at, account_id, user_id, fund_id, symbol, order_id,
                 owner_id, owner_epoch, request_digest, state)
                VALUES (301,302,?,101,201,'BTCUSDT','9001','legacy-owner',3,
                        'cancel-digest','DISPATCHED') RETURNING *`,
		},
		{
			model: &portfoliomodelv1.VenueAccountSnapshot{},
			statement: `INSERT INTO himalaya_projections
                (create_at, update_at, account_id, snapshot, publisher_owner_id, publisher_epoch)
                VALUES (401,402,?,'{"available":"123"}','legacy-publisher',4) RETURNING *`,
		},
		{
			model: &executionmodelv1.ExecutionRecoveryBaseline{},
			statement: `INSERT INTO himalaya_development_baselines
                (create_at, update_at, account_id, request, request_digest, evidence_digest,
                 snapshot, legacy_notional, minimum_observed_notional)
                VALUES (501,502,?,'{}','baseline-digest','evidence-digest',
                        '{"available":"123"}',50,900) RETURNING *`,
		},
	}
	for _, fixture := range fixtures {
		if err := db.WithContext(ctx).Model(fixture.model).Raw(fixture.statement, account).Scan(fixture.model).Error; err != nil {
			t.Fatal(err)
		}
		before := proto.Clone(fixture.model)
		checks = append(checks, func() {
			after := before.ProtoReflect().New().Interface()
			if err := db.WithContext(ctx).Model(after).First(after).Error; err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(before, after) {
				t.Errorf("venue model upgrade changed %T: before=%v after=%v", before, before, after)
			}
		})
	}
	return func() {
		t.Helper()
		for _, check := range checks {
			check()
		}
	}
}

func assertVenueUpgradeRollback(ctx context.Context, t *testing.T, db *gorm.DB, account string, budget *commonmodelv1.TradingAccountPolicy) {
	t.Helper()
	// A conflicting destination must roll the entire upgrade back, including the ledger rename.
	if err := db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
		Exec("CREATE TABLE venue_order_intents (id bigint PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := venuemigrate.Apply(ctx, db); err == nil {
		t.Fatal("venue upgrade committed despite a conflicting destination")
	}
	var history struct {
		Legacy  bool
		Current bool
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).
		Raw("SELECT to_regclass('himalaya_schema_migrations') IS NOT NULL AS legacy, to_regclass('venue_schema_migrations') IS NOT NULL AS current").Scan(&history).Error; err != nil || !history.Legacy || history.Current {
		t.Fatalf("failed venue upgrade did not restore legacy history: %+v, %v", history, err)
	}
	var rolledBack commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).
		Raw("SELECT * FROM himalaya_budgets WHERE account_id = ?", account).Scan(&rolledBack).Error; err != nil || !proto.Equal(budget, &rolledBack) {
		t.Fatalf("failed venue upgrade changed retained risk: %v", err)
	}
	if err := db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).Exec("DROP TABLE venue_order_intents").Error; err != nil {
		t.Fatal(err)
	}

}
