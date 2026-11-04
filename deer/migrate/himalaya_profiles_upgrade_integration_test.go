//go:build integration

package migrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestHimalayaDatabaseProfileUpgradePreservesVersionFiveRiskAndTape(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("venue/sql/000[1-5]_*.sql")
	if err != nil || len(paths) != 5 {
		t.Fatalf("version five migration fixture: %v", err)
	}
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec(string(data)).Error; err != nil {
			t.Fatal(err)
		}
		// Explicit legacy fixture: the removed ORM types are not recreated.
		if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).
			Exec("INSERT INTO himalaya_schema_migrations (create_at, update_at, version, checksum) VALUES (1, 1, ?, ?)", int64(i+1), fmt.Sprintf("%x", sha256.Sum256(data))).Error; err != nil {
			t.Fatal(err)
		}
	}
	account := native.ScopeKey(101, 201)
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Create(&commonmodelv1.ExecutionShardLease{AccountId: account}).Error; err != nil {
		t.Fatal(err)
	}
	// A version-five model must not select newly added columns during insertion.
	budget := &commonmodelv1.TradingAccountPolicy{
		AccountId:            account,
		UserId:               101,
		FundId:               201,
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		ObservedNotional:     "900",
		ReservedNotional:     "50",
		AvailableQuote:       "123",
		Leverage:             10,
		Enabled:              true,
		CutoverCompleted:     true,
	}
	// Seed a historical row through reviewed migration SQL, without a legacy model.
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Raw(`
        INSERT INTO himalaya_budgets (create_at, update_at, account_id, user_id, fund_id,
            maximum_notional, maximum_order_notional, observed_notional, reserved_notional,
            available_quote, leverage, enabled, cutover_completed, snapshot_expires_at)
        VALUES (101, 102, ?, 101, 201, 1000, 100, 900, 50, 123, 10, true, true, 999999)
        RETURNING *`, account).Scan(budget).Error; err != nil {
		t.Fatal(err)
	}

	checkVenueRecords := seedVenueUpgradeRecords(ctx, t, db, account)
	audit := &executionmodelv1.SyntheticTapeIntent{
		RunId:        "schema-five",
		Symbol:       "BTCUSDT",
		PolicyDigest: fmt.Sprintf("%x", sha256.Sum256([]byte("retained policy"))),
		Notional:     "1",
		Plan: &commonv1.SyntheticTapePlan{
			RunId:    "schema-five",
			Symbol:   "BTCUSDT",
			Side:     string(domain.SideBuy),
			Price:    "100",
			Quantity: "0.01",
		},
		Result: &commonv1.SyntheticTapeResult{State: native.SyntheticUnknown},
	}
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Create(audit).Error; err != nil {
		t.Fatal(err)
	}
	var before executionmodelv1.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).First(&before).Error; err != nil {
		t.Fatal(err)
	}

	assertVenueUpgradeRollback(ctx, t, db, account, budget)
	if err := venuemigrate.Check(ctx, db); !errors.Is(err, venuemigrate.ErrHistory) {
		t.Fatalf("old schema accepted before explicit upgrade: %v", err)
	}
	for range 2 {
		if err := venuemigrate.Apply(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if err := venuemigrate.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	checkVenueRecords()
	var after executionmodelv1.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).First(&after).Error; err != nil || !proto.Equal(&before, &after) {
		t.Fatalf("database profile upgrade rewrote tape audit: %v", err)
	}
	var retained commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).First(&retained).Error; err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ raw, want string }{
		{retained.ObservedNotional, "900"},
		{retained.ReservedNotional, "50"},
		{retained.AvailableQuote, "123"},
		{retained.MaximumNotional, "1000"},
	} {
		parsed, err := domain.ParseLedgerDecimal(value.raw, false, true)
		if err != nil || parsed.String() != value.want {
			t.Fatalf("retained financial value changed: %s %v", value.raw, err)
		}
	}
	if retained.Id != budget.Id || retained.CreateAt != budget.CreateAt || !retained.Enabled || !retained.CutoverCompleted || retained.Symbol != "" || retained.MarketDataSource != "" {
		t.Fatal("upgrade guessed a business binding or reset retained authority")
	}
}
