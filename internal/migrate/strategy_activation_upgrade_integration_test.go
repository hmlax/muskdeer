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

func TestStrategyActivationUpgradeRetainsExistingActiveBudget(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:26]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := identityInstance(instrument.Id, "activation-upgrade-existing", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	instance.DesiredState = string(domain.StrategyStateStopped)
	instance.ActiveConfigVersion = proto.Int64(1)
	instance.ActiveMarketDataSource = proto.String(string(domain.MarketSourceBinance))
	config := storedParameterFixture(domain.StrategyTemplateMarketMaker)
	account := &modelv1.AccountBudget{
		AccountId:        instance.AccountId,
		Asset:            "USDT",
		LimitNotional:    "500",
		ReservedNotional: "0",
		Enabled:          true,
		Revision:         1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		config.InstanceId = instance.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(config).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
		StrategyInstanceId: instance.Id,
		AccountId:          instance.AccountId,
		LimitNotional:      "300",
		ReservedNotional:   "123.000000000000000001",
		Enabled:            false,
		Revision:           7,
	}).Error; err != nil {
		t.Fatal(err)
	}
	before := activationBudget(ctx, t, db, instance.Id)
	if version, err := Apply(ctx, db); err != nil || version != 31 {
		t.Fatalf("activation constraint upgrade failed: %d, %v", version, err)
	}
	var actual modelv1.StrategyInstance
	var actualConfig modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).First(&actual).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("id = ?", config.Id).First(&actualConfig).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(instance, &actual) {
		t.Fatalf("upgrade changed instance: before=%v, after=%v", instance, &actual)
	}
	if !proto.Equal(config, &actualConfig) || !proto.Equal(before, activationBudget(ctx, t, db, instance.Id)) {
		t.Fatal("upgrade rewrote historical parameters, capacity, reservation or disabled state")
	}
}
