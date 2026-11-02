//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminFlatteningProgressUpgradeQueuesExistingConfirmationWithoutInventedWork(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:36]); err != nil {
		t.Fatal(err)
	}
	core, submit := coreFixtureWithCommand(ctx, t, db, gatewayCommandAfterSchema(ctx, t, db))
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(&modelv1.AccountBudget{
		AccountId:        submit.Input.AccountID,
		Asset:            "USDT",
		LimitNotional:    "300",
		ReservedNotional: "0",
		Enabled:          true,
		Revision:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	seedGlobalRiskBudgets(ctx, t, db, submit.Input.InstrumentID)
	seedAdmissionStrategy(ctx, t, db, submit.Input)
	portfolioFixture(ctx, t, db, submit)
	operator := adminControlSessionFixture(ctx, t, db)
	_, _, confirmation := flattenSchemaCandidate(ctx, t, db, operator, submit)
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(confirmation).Error; err != nil {
		t.Fatal(err)
	}
	if db.WithContext(ctx).Model(&modelv1.AdminAccountFlatteningTask{}).Migrator().HasTable(&modelv1.AdminAccountFlatteningTask{}) {
		t.Fatal("upgrade fixture silently advanced beyond schema36")
	}
	// Compare persisted PostgreSQL values on both sides of the upgrade. Numeric
	// columns retain their declared scale, unlike the original Go create input.
	var original modelv1.AdminAccountFlattening
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", confirmation.Id).Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, confirmation.StrategyInstanceId)
	budget := activationBudget(ctx, t, db, confirmation.StrategyInstanceId)
	for range 2 {
		if version, err := Apply(ctx, db); err != nil || version != 37 {
			t.Fatal("schema36 progress migration or checked replay failed", err)
		}
		row := flattenTaskRow(ctx, t, db, confirmation.Id)
		if row.State != string(domain.AdminFlattenQueued) || row.Revision != 1 || row.OwnerId != nil || row.OwnerEpoch != nil || row.ClaimedAt != nil ||
			row.Id <= 0 || row.CreateAt <= 0 || row.UpdateAt < row.CreateAt || row.DeleteAt != nil || row.StrategyInstanceId != confirmation.StrategyInstanceId ||
			row.AccountId != confirmation.AccountId || row.InstrumentId != confirmation.InstrumentId {
			t.Fatal("upgrade fabricated work/ownership or lost its confirmed scope")
		}
		assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAccountFlatteningTask{}, 1)
	}
	var after modelv1.AdminAccountFlattening
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Where("id = ?", confirmation.Id).Take(&after).Error; err != nil || !proto.Equal(&original, &after) {
		t.Fatal("work migration rewrote immutable confirmation", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(budget, activationBudget(ctx, t, db, before.Id)) {
		t.Fatal("queued marker upgrade changed old strategy or risk budget")
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}
