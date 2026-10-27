//go:build integration

package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestGatewayFencesBothOwnersAtCommandCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := gatewayCommandFixture(ctx, t, db)
	if err := gatewayOrderFixture(ctx, db, command); err != nil {
		t.Fatal(err)
	}
	var order modelv1.SimulatorOrder
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).First(&order).Error; err != nil {
		t.Fatal(err)
	}
	price, err := decimal.NewFromString(order.Price)
	if err != nil || !price.Equal(decimal.RequireFromString("99999999999999999999.123456789012345678")) || order.Id <= 0 || order.CreateAt <= 0 || order.UpdateAt <= 0 || order.DeleteAt != nil {
		t.Fatalf("gateway order lost numeric/common fields: %v", &order)
	}
	for _, change := range []func(*modelv1.SimulatorCommand){
		func(row *modelv1.SimulatorCommand) { row.OwnerEpoch++ },
		func(row *modelv1.SimulatorCommand) { row.ExecutionEpoch++ },
		func(row *modelv1.SimulatorCommand) { row.OwnerId += "-stale" },
		func(row *modelv1.SimulatorCommand) { row.ExecutionOwnerId += "-stale" },
		func(row *modelv1.SimulatorCommand) { row.DeadlineAt = 1 },
	} {
		candidate := proto.Clone(command).(*modelv1.SimulatorCommand)
		candidate.Id = 0
		candidate.CommandKey += "-invalid"
		candidate.ClientOrderId += "-invalid"
		change(candidate)
		assertGatewayFence(t, gatewayOrderFixture(ctx, db, candidate))
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertGatewayTakeover(ctx, t, db, command)
}

func TestGatewayRevalidatesAfterEffectsAreStaged(t *testing.T) {
	for _, key := range []domain.LeaseKind{domain.LeaseKindStrategy, domain.LeaseKindExecution} {
		t.Run(string(key), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := gatewayCommandFixture(ctx, t, db)
			err := db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Transaction(func(tx *gorm.DB) error {
				if err := stageGatewayOrder(ctx, tx, command); err != nil {
					t.Fatalf("valid command failed before controlled expiry: %v", err)
				}
				// A controlled database schedule, after both writes but before
				// commit. An admission-only or immediate trigger would miss it.
				leaseKey := domain.LeaseKey{Kind: key}
				if key == domain.LeaseKindStrategy {
					leaseKey.StrategyID = command.StrategyInstanceId
				} else {
					leaseKey.AccountID = command.AccountId
				}
				expireLeaseFixture(ctx, t, tx, leaseKey)
				return nil
			})
			assertGatewayFence(t, err)
			assertGatewayOrderCount(ctx, t, db, 0)
			var commands int64
			if err := db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Count(&commands).Error; err != nil || commands != 0 {
				t.Fatalf("rejected authority committed command: %d, %v", commands, err)
			}
		})
	}
}

func TestGatewayCannotCommitUnusedAuthorityOrAcceptNumericNaN(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := gatewayCommandFixture(ctx, t, db)
	err := db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Create(command).Error
	assertDatabaseCode(t, err, "23514")
	for _, column := range []string{"price_tick", "quantity_step", "min_notional"} {
		err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ?", command.InstrumentId).UpdateColumn(column, "NaN").Error
		assertDatabaseCode(t, err, "23514")
	}
	command.Id = 0
	if err := gatewayOrderFixture(ctx, db, command); err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("id = ?", command.Id).UpdateColumn("deadline_at", command.DeadlineAt+1).Error
	assertDatabaseCode(t, err, "23514")
	for _, column := range []string{"price", "quantity", "filled_quantity", "frozen_amount"} {
		err := db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("initial_command_id = ?", command.Id).UpdateColumn(column, "NaN").Error
		assertDatabaseCode(t, err, "23514")
	}
}

func assertGatewayTakeover(ctx context.Context, t *testing.T, db *gorm.DB, original *modelv1.SimulatorCommand) {
	t.Helper()
	store, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: original.StrategyInstanceId,
	}
	expireLeaseFixture(ctx, t, db, key)
	newOwner, err := ownerid.New(ctx, "takeover")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(ctx, key, newOwner, 30*time.Second)
	if err != nil || lease.Epoch <= original.OwnerEpoch {
		t.Fatalf("gateway successor lease = %v, %v", lease, err)
	}
	stale := proto.Clone(original).(*modelv1.SimulatorCommand)
	stale.Id = 0
	stale.CommandKey += "-old-process"
	stale.ClientOrderId += "-old-process"
	assertGatewayFence(t, gatewayOrderFixture(ctx, db, stale))
	stale.OwnerId = lease.OwnerID
	stale.OwnerEpoch = lease.Epoch
	if err := gatewayOrderFixture(ctx, db, stale); err != nil {
		t.Fatal(err)
	}
	assertGatewayOrderCount(ctx, t, db, 2)
}

func gatewayCommandFixture(ctx context.Context, t *testing.T, db *gorm.DB) *modelv1.SimulatorCommand {
	t.Helper()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	strategyID := leaseStrategyFixture(ctx, t, db)
	var strategy modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&strategy, strategyID).Error; err != nil {
		t.Fatal(err)
	}
	parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
	parameters.MakerParameters.InventoryHardLimit = "20"
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ?", strategyID, 1).
		Select("ParametersSchemaVersion", "Template", "MakerParameters").Updates(parameters).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).
		Select("State", "DesiredRunning", "ActiveConfigVersion", "ActiveMarketDataSource").Updates(&modelv1.StrategyInstance{
		State:                  string(domain.StrategyStateRunning),
		DesiredRunning:         true,
		ActiveConfigVersion:    proto.Int64(1),
		ActiveMarketDataSource: proto.String(string(domain.MarketSourceBinance)),
	}).Error; err != nil {
		t.Fatal(err)
	}
	strategyLease := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: strategyID,
	})
	executionLease := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: strategy.AccountId,
	})
	return &modelv1.SimulatorCommand{
		AccountId:          strategy.AccountId,
		CommandKey:         "gateway-submit-1",
		Action:             "SUBMIT", // External database contract fixture.
		ClientOrderId:      "gateway-client-1",
		StrategyInstanceId: strategyID,
		ConfigVersion:      1,
		OwnerId:            strategyLease.OwnerID,
		OwnerEpoch:         strategyLease.Epoch,
		ExecutionOwnerId:   executionLease.OwnerID,
		ExecutionEpoch:     executionLease.Epoch,
		InstrumentId:       strategy.InstrumentId,
		Fingerprint:        strings.Repeat("a", 64),
		DeadlineAt:         time.Now().Add(10 * time.Second).UnixMilli(),
	}
}

func acquireGatewayLease(ctx context.Context, t *testing.T, db *gorm.DB, key domain.LeaseKey) domain.LeaseToken {
	t.Helper()
	owner, err := ownerid.New(ctx, "gateway-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(ctx, key, owner, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func gatewayOrderFixture(ctx context.Context, db *gorm.DB, command *modelv1.SimulatorCommand) error {
	return db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Transaction(func(tx *gorm.DB) error {
		return stageGatewayOrder(ctx, tx, command)
	})
}

func stageGatewayOrder(ctx context.Context, tx *gorm.DB, command *modelv1.SimulatorCommand) error {
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Create(command).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Create(&modelv1.SimulatorOrder{
		AccountId:          command.AccountId,
		ClientOrderId:      command.ClientOrderId,
		InitialCommandId:   command.Id,
		LastCommandId:      command.Id,
		StrategyInstanceId: command.StrategyInstanceId,
		ConfigVersion:      command.ConfigVersion,
		InstrumentId:       command.InstrumentId,
		Side:               string(domain.SideBuy),
		Price:              "99999999999999999999.123456789012345678",
		Quantity:           "0.2",
		FilledQuantity:     "0",
		TimeInForce:        string(domain.TimeInForceGTC),
		State:              string(domain.OrderStateLive),
		CancelState:        string(domain.CancelStateNone),
		FrozenAsset:        "USDT",
		FrozenAmount:       "19999999999999999999.8246913578024691356",
		QuoteFeeRate:       "0",
		Revision:           1,
	}).Error
}

func assertGatewayFence(t *testing.T, err error) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || databaseError.ConstraintName != "simulator_command_fence" {
		t.Fatalf("final gateway fence = %v", err)
	}
}

func assertDatabaseCode(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		t.Fatalf("database rejection = %v, expected %s", err, code)
	}
}

func assertGatewayOrderCount(ctx context.Context, t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Count(&count).Error; err != nil || count != expected {
		t.Fatalf("gateway order effects = %d, %v; expected %d", count, err, expected)
	}
}
