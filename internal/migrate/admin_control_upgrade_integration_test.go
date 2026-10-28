//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminControlUpgradeRetainsExistingRequestedConfiguration(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:24]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := identityInstance(instrument.Id, "control-upgrade-paused", domain.StrategyTemplateGrid, domain.MarketSourceOKX)
	instance.State = string(domain.StrategyStatePaused)
	instance.DesiredState = string(domain.StrategyStatePaused)
	instance.DesiredRunning = false
	parameters := storedParameterFixture(domain.StrategyTemplateGrid)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		parameters.InstanceId = instance.Id
		parameters.MarketDataSource = string(domain.MarketSourceOKX)
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	if version, err := Apply(ctx, db); err != nil || version != 26 {
		t.Fatal("operation ledger upgrade failed", err)
	}
	var actual modelv1.StrategyInstance
	var config modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).Take(&actual).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("id = ?", parameters.Id).Take(&config).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(instance, &actual) || !proto.Equal(parameters, &config) {
		t.Fatal("upgrade changed requested state, source or configuration")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminUser{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}
