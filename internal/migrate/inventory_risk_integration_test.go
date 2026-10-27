//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestWorstInventoryRetainsUnknownBuysAndDoesNotNetUnconfirmedSells(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	activateInventoryVersion(ctx, t, db, &submit, "12")
	raiseInventoryTestBudgets(ctx, t, db, submit.Input.AccountID)
	_, store := executionFixture(t, db)
	accepted, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(ctx, accepted, submit.Input.Authority, submit.ExecutionAuthority, submit.Input.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Unknown(ctx, prepared, domain.IntentErrorOutcomeUnknown); err != nil {
		t.Fatal(err)
	}
	sell := submit.Input
	sell.IdempotencyKey = "unconfirmed-inventory-sell"
	sell.Side = domain.SideSell
	if _, err := intents.Accept(ctx, sell, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	more := submit.Input
	more.IdempotencyKey = "worst-inventory-buy"
	more.Quantity = decimal.NewFromInt(1)
	if _, err := intents.Accept(ctx, more, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("unknown buy or unconfirmed sell understated worst inventory: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "400")
}

func TestSharedAccountUsesStrictestActiveInventoryLimit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	intents, first := admissionFixture(ctx, t, db)
	second := admissionSibling(ctx, t, db, first)
	activateInventoryVersion(ctx, t, db, &first, "12")
	raiseInventoryTestBudgets(ctx, t, db, first.Input.AccountID)
	if _, err := intents.Accept(ctx, first.Input, first.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	if _, err := intents.Accept(ctx, second.Input, second.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("another strategy bypassed shared inventory hard limit: %v", err)
	}
	assertReservedBudget(ctx, t, db, first.Input.AccountID, "200")
}

func TestInventoryActivationCannotLowerHardLimitBelowOutstandingExposure(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	err := insertInventoryVersion(ctx, db, submit, "11")
	assertDatabaseCode(t, err, "23514")
	var strategy modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&strategy, submit.Input.Authority.StrategyID).Error; err != nil || strategy.GetActiveConfigVersion() != 1 {
		t.Fatalf("failed activation changed current version: %v, %v", &strategy, err)
	}
	var row modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ?", submit.Input.Authority.StrategyID, 1).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	changed := proto.CloneOf(row.MakerParameters)
	changed.InventoryHardLimit = "1000"
	err = db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("id = ?", row.Id).Update("maker_parameters", changed).Error
	assertDatabaseCode(t, err, "23514")
}

func TestFinalInventoryFenceRollsBackAlreadyStagedFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	const callbackName = "muskdeer:test:staged-base-inventory-change"
	if err := db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
			return
		}
		err := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", submit.Input.AccountID, "BTC").UpdateColumn("available", "19").Error
		tx.Error = errors.Join(tx.Error, err)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	rejected, err := intentServiceFixture(t, store, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || rejected.State != domain.OrderStateRejected || rejected.ErrorCode != domain.IntentErrorRiskDenied {
		t.Fatalf("final inventory change escaped atomic fence: %+v, %v", rejected, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func activateInventoryVersion(ctx context.Context, t *testing.T, db *gorm.DB, submit *domain.GatewaySubmit, hard string) {
	t.Helper()
	if err := insertInventoryVersion(ctx, db, *submit, hard); err != nil {
		t.Fatal(err)
	}
	submit.Input.Authority.ConfigVersion = 2
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).UpdateColumn("config_version", 2).Error; err != nil {
		t.Fatal(err)
	}
}

func insertInventoryVersion(ctx context.Context, db *gorm.DB, submit domain.GatewaySubmit, hard string) error {
	return db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = submit.Input.Authority.StrategyID
		parameters.Version = 2
		parameters.MakerParameters.InventoryHardLimit = hard
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).Select("RequestedConfigVersion", "ActiveConfigVersion").Updates(&modelv1.StrategyInstance{
			RequestedConfigVersion: 2,
			ActiveConfigVersion:    proto.Int64(2),
		}).Error
	})
}

func raiseInventoryTestBudgets(ctx context.Context, t *testing.T, db *gorm.DB, accountID string) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", accountID).UpdateColumn("limit_notional", "1000").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("account_id = ?", accountID).UpdateColumn("limit_notional", "1000").Error; err != nil {
		t.Fatal(err)
	}
}
