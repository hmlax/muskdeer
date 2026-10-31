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

func TestAdminRiskUpgradeRetainsExistingBudgetAndIndependentInitialRevision(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:28]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := identityInstance(instrument.Id, "risk-upgrade-existing", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	instance.State = string(domain.StrategyStatePaused)
	instance.DesiredState = string(domain.StrategyStatePaused)
	instance.ActiveConfigVersion = proto.Int64(1)
	instance.ActiveMarketDataSource = proto.String(string(domain.MarketSourceBinance))
	config := storedParameterFixture(domain.StrategyTemplateMarketMaker)
	account := &modelv1.AccountBudget{
		AccountId:        instance.AccountId,
		Asset:            "USDT",
		LimitNotional:    "500",
		ReservedNotional: "0",
		Enabled:          false,
		Revision:         19,
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
		Revision:           97,
	}).Error; err != nil {
		t.Fatal(err)
	}
	before := activationBudget(ctx, t, db, instance.Id)
	if version, err := Apply(ctx, db); err != nil || version != 33 {
		t.Fatal("risk control upgrade failed", err)
	}
	if !proto.Equal(before, activationBudget(ctx, t, db, instance.Id)) || !proto.Equal(instance, publicationInstance(ctx, t, db, instance.Id)) {
		t.Fatal("upgrade rewrote a legacy disabled flag, reservation, amount or runtime target")
	}
	actor := adminControlSessionFixture(ctx, t, db)
	control := riskControlFixture(t, db)
	state, err := control.State(ctx, actor.Token, instance.Id)
	if err != nil || state.Enabled || state.Revision != 1 || state.ReservedNotional.String() != "123.000000000000000001" {
		t.Fatal("initial risk management version was coupled to legacy financial revision", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}
