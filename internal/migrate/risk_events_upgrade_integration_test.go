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

func TestRiskSnapshotEventUpgradeStartsBaselineWithoutInventingOldTransitions(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:30]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	for _, retired := range []bool{false, true} {
		source := riskEventLegacySource(ctx, t, db, instrument.Id, retired)
		if source.MarketSequence != 9007199254740993 || source.Revision != 9007199254740994 {
			t.Fatal("legacy source fixture lost int64 precision")
		}
	}
	var before []*modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Order("id ASC").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if version, err := Apply(ctx, db); err != nil || version != 33 {
		t.Fatal("risk diagnostic recording upgrade failed", err)
	}
	for _, source := range before {
		events := riskEventRows(ctx, t, db, source.StrategyInstanceId)
		if len(events) != 1 || events[0].Kind != string(domain.RiskSnapshotEventBaseline) || events[0].RiskSnapshotId != source.Id || events[0].RiskSnapshotRevision != source.Revision || events[0].MarketSequence != source.MarketSequence || events[0].SnapshotDeleted != (source.DeleteAt != nil) || events[0].CreateAt <= 0 || events[0].CreateAt != events[0].UpdateAt || events[0].DeleteAt != nil {
			t.Fatal("migration invented an old change or lost baseline precision/retirement")
		}
		if !proto.Equal(source, riskEventSourceRow(ctx, t, db, source.StrategyInstanceId)) {
			t.Fatal("history upgrade mutated an existing risk source")
		}
	}
	if version, err := Apply(ctx, db); err != nil || version != 33 {
		t.Fatal("checked migration replay failed", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
}

func riskEventLegacySource(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64, retired bool) *modelv1.RiskSnapshot {
	t.Helper()
	key := "risk-history-existing-active"
	if retired {
		key = "risk-history-existing-retired"
	}
	instance := identityInstance(instrumentID, key, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	config := storedParameterFixture(domain.StrategyTemplateMarketMaker)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		config.InstanceId = instance.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(config).Error
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	source := &modelv1.RiskSnapshot{
		StrategyInstanceId: instance.Id,
		AccountId:          instance.AccountId,
		InstrumentId:       instrumentID,
		ConfigVersion:      config.Version,
		MarketDataSource:   config.MarketDataSource,
		MarketEpoch:        1,
		MarketSequence:     9007199254740993,
		MarketMaximumAgeMs: 5000,
		ObservedAt:         now,
		ExpiresAt:          now + 1000,
		Revision:           9007199254740994,
		ReasonCode:         string(domain.RiskReasonNotEvaluated),
	}
	if retired {
		source.DeleteAt = &now
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Create(source).Error; err != nil {
		t.Fatal(err)
	}
	return source
}
