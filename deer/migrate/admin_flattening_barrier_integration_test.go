//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminFlatteningPendingBlocksRiskRecoveryBudgetAndCachedGrants(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, submit, row := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	instance := publicationInstance(ctx, t, db, row.StrategyInstanceId)
	if _, err := riskControlFixture(t, db).Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(instance)); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("unfinished flattening allowed an otherwise valid administrator recovery", err)
	}
	assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&strategymodelv1.StrategyBudget{}).Where("strategy_instance_id = ?", row.StrategyInstanceId).UpdateColumn("enabled", true).Error, "admin_flatten_pending")
	assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&executionmodelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", row.StrategyInstanceId).UpdateColumn("allow_new_risk", true).Error, "admin_flatten_pending")
	state, err := riskControlFixture(t, db).State(ctx, admin.Token, row.StrategyInstanceId)
	if err != nil || state.Enabled || state.Revision != row.ExpectedRiskRevision {
		t.Fatal("rejected recovery changed the prior risk state", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 1)
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.AdminStrategyRiskChange{}, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningPendingConflictsWithCreationAndPublicationWithoutPartialEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, submit, row := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, row.StrategyInstanceId)
	creator, err := repository.NewAdminStrategyCreator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(ctx, admin.Token, admin.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("pending account flattening allowed a new shared strategy", err)
	}
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	creation := adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceOKX)
	request := publicationRequest(creation, before.Id, before.Revision, before.RequestedConfigVersion)
	if _, err := publisher.Publish(ctx, admin.Token, admin.CSRF, request); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("pending account flattening allowed parameter/source publication", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("rejected publication left a target, source or version change")
	}
	for _, entity := range []any{&strategymodelv1.StrategyInstance{}, &strategymodelv1.StrategyConfigVersion{}, &strategymodelv1.StrategyBudget{}, &executionmodelv1.AdminAccountFlattening{}} {
		assertPortfolioRowCount(ctx, t, db, entity, 1)
	}
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.AdminStrategyCreation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.AdminStrategyPublication{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningPendingRejectsConfigurationInEitherTransactionOrder(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, _, row := flattenSchemaFixture(ctx, t, db)
	var original strategymodelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ?", row.StrategyInstanceId, 1).Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	for _, flattenFirst := range []bool{false, true} {
		err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Transaction(func(tx *gorm.DB) error {
			confirmation := proto.CloneOf(row)
			config := proto.CloneOf(&original)
			config.Id, config.Version = 0, 2
			if flattenFirst {
				if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(confirmation).Error; err != nil {
					return err
				}
			}
			if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Create(config).Error; err != nil {
				return err
			}
			if !flattenFirst {
				return tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(confirmation).Error
			}
			return nil
		})
		assertEmergencySchemaConstraint(t, err, "admin_flatten_pending")
		assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
		assertPortfolioRowCount(ctx, t, db, &strategymodelv1.StrategyConfigVersion{}, 1)
	}
}
