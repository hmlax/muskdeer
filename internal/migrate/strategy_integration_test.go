//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/postgres"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const (
	identityFixtureCode  = "schema-v2-fixture"
	identityFixtureActor = "fixture-operator"
)

func TestV1DatabaseUpgradesAndRetainsIndependentSourceVersions(t *testing.T) {
	base, cfg := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	assertIdentityUpgrade(ctx, t, base)
	writer, err := postgres.New(ctx, cfg, "strategy-identity-writer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	db := writer.GORM()
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	var maker, grid *modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		var err error
		maker, err = insertIdentity(ctx, tx, instrument.Id, "maker-binance", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
		if err != nil {
			return err
		}
		grid, err = insertIdentity(ctx, tx, instrument.Id, "grid-okx", domain.StrategyTemplateGrid, domain.MarketSourceOKX)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if maker.Id <= 0 || grid.Id <= maker.Id || maker.MarketDataSource != string(domain.MarketSourceBinance) || grid.MarketDataSource != string(domain.MarketSourceOKX) {
		t.Fatal("initial template/source identities were not independent")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		version := &modelv1.StrategyConfigVersion{
			InstanceId:       maker.Id,
			Version:          2,
			MarketDataSource: string(domain.MarketSourceOKX),
			CodeVersion:      identityFixtureCode,
			CreatedBy:        identityFixtureActor,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
			return err
		}
		// This is a persistence fixture, not a claim that runtime switching is
		// implemented. Active input deliberately retains the previous version.
		return tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND revision = ?", maker.Id, 1).
			Select("RequestedConfigVersion", "MarketDataSource", "ActiveConfigVersion", "ActiveMarketDataSource", "State", "DesiredRunning", "Revision").
			Updates(&modelv1.StrategyInstance{
				RequestedConfigVersion: 2,
				MarketDataSource:       string(domain.MarketSourceOKX),
				ActiveConfigVersion:    proto.Int64(1),
				ActiveMarketDataSource: proto.String(string(domain.MarketSourceBinance)),
				State:                  string(domain.StrategyStatePaused),
				DesiredRunning:         false,
				Revision:               2,
			}).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := postgres.New(ctx, cfg, "strategy-identity-restarted-reader")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	var restoredGrid modelv1.StrategyInstance
	if err := reader.GORM().WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&restoredGrid, grid.Id).Error; err != nil {
		t.Fatal(err)
	}
	if restoredGrid.MarketDataSource != string(domain.MarketSourceOKX) || restoredGrid.RequestedConfigVersion != 1 || restoredGrid.ActiveConfigVersion != nil {
		t.Fatal("another instance was changed by the first instance's source update")
	}
	assertRestoredIdentity(ctx, t, reader.GORM(), maker.Id, instrument)
}

func TestStrategySourceForeignKeysRejectMismatchAndCandidates(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	for _, source := range []domain.MarketSource{domain.MarketSourceBitget, domain.MarketSourceBybit, domain.MarketSourceBinance} {
		err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			row := identityInstance(instrument.Id, "rejected-source", domain.StrategyTemplateGrid, source)
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(row).Error; err != nil {
				return err
			}
			return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(&modelv1.StrategyConfigVersion{
				InstanceId:       row.Id,
				Version:          1,
				MarketDataSource: string(domain.MarketSourceOKX),
				CodeVersion:      identityFixtureCode,
				CreatedBy:        identityFixtureActor,
			}).Error
		})
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" && databaseError.Code != "23503" {
			t.Fatalf("source mismatch/candidate was accepted: %v", err)
		}
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected source left partial state: %d, %v", count, err)
	}
}

func identityInstrument() *modelv1.Instrument {
	return &modelv1.Instrument{
		ExecutionVenue:  domain.ExecutionVenueSimulator,
		Symbol:          "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		PriceTick:       "0.01",
		QuantityStep:    "0.00000001",
		MinNotional:     "99999999999999999999.123456789012345678",
		Status:          domain.InstrumentStatusTrading,
		MetadataVersion: 1,
	}
}

func identityInstance(instrumentID int64, key string, template domain.StrategyTemplate, source domain.MarketSource) *modelv1.StrategyInstance {
	return &modelv1.StrategyInstance{
		InstanceKey:            key,
		Template:               string(template),
		AccountId:              "fixture-shared-account",
		InstrumentId:           instrumentID,
		CodeVersion:            identityFixtureCode,
		Revision:               1,
		State:                  string(domain.StrategyStateCreated),
		DesiredRunning:         false,
		RequestedConfigVersion: 1,
		MarketDataSource:       string(source),
	}
}

func insertIdentity(ctx context.Context, tx *gorm.DB, instrumentID int64, key string, template domain.StrategyTemplate, source domain.MarketSource) (*modelv1.StrategyInstance, error) {
	row := identityInstance(instrumentID, key, template, source)
	row.DesiredRunning = template == domain.StrategyTemplateMarketMaker
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(row).Error; err != nil {
		return nil, err
	}
	err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(&modelv1.StrategyConfigVersion{
		InstanceId:       row.Id,
		Version:          1,
		MarketDataSource: string(source),
		CodeVersion:      identityFixtureCode,
		CreatedBy:        identityFixtureActor,
	}).Error
	return row, err
}

func assertIdentityUpgrade(ctx context.Context, t *testing.T, base *gorm.DB) {
	t.Helper()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, base, definitions[:1]); err != nil {
		t.Fatal(err)
	}
	var original modelv1.SchemaMigration
	if err := base.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	if version, err := Apply(ctx, base); err != nil || version != 18 {
		t.Fatalf("v1 upgrade = %d, %v", version, err)
	}
	var retained modelv1.SchemaMigration
	if err := base.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).First(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&original, &retained) {
		t.Fatal("upgrade rewrote existing migration history")
	}
}

func assertRestoredIdentity(ctx context.Context, t *testing.T, db *gorm.DB, makerID int64, instrument *modelv1.Instrument) {
	t.Helper()
	var restored modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&restored, makerID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.MarketDataSource != string(domain.MarketSourceOKX) || restored.GetActiveMarketDataSource() != string(domain.MarketSourceBinance) || restored.GetActiveConfigVersion() != 1 || restored.RequestedConfigVersion != 2 || restored.DesiredRunning {
		t.Fatalf("restart lost requested/active source identity: %v", &restored)
	}
	var history []*modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ?", makerID).Order("version ASC").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].MarketDataSource != string(domain.MarketSourceBinance) || history[1].MarketDataSource != string(domain.MarketSourceOKX) {
		t.Fatalf("source history changed: %v", history)
	}
	// The active historical version is retained. Nullable storage semantics
	// are exercised below on the inactive legacy draft, without retiring the
	// version that still supplies the instance's actual input.
	err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("id = ?", history[0].Id).UpdateColumn("delete_at", time.Now().UnixMilli()).Error
	assertDatabaseCode(t, err, "23514")
	var storedInstrument modelv1.Instrument
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).First(&storedInstrument, instrument.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedInstrument.ExecutionVenue != domain.ExecutionVenueSimulator || !decimal.RequireFromString(storedInstrument.MinNotional).Equal(decimal.RequireFromString(instrument.MinNotional)) {
		t.Fatal("source selection changed execution venue or decimal metadata")
	}
	for _, record := range []struct {
		model any
		id    int64
	}{
		{
			model: &modelv1.Instrument{},
			id:    instrument.Id,
		},
		{
			model: &modelv1.StrategyInstance{},
			id:    makerID,
		},
		{
			model: &modelv1.StrategyConfigVersion{},
			id:    history[1].Id,
		},
	} {
		assertIdentityCommonColumns(ctx, t, db, record.model, record.id)
	}
}

func assertIdentityCommonColumns(ctx context.Context, t *testing.T, db *gorm.DB, entity any, id int64) {
	t.Helper()
	columns, err := db.WithContext(ctx).Model(entity).Migrator().ColumnTypes(entity)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		switch column.Name() {
		case "id":
			if primary, valid := column.PrimaryKey(); !valid || !primary {
				t.Fatal("generated Id is not the database primary key")
			}
		case "create_at", "update_at":
			if nullable, valid := column.Nullable(); !valid || nullable {
				t.Fatalf("%s common time is nullable", column.Name())
			}
		case "delete_at":
			if nullable, valid := column.Nullable(); !valid || !nullable {
				t.Fatal("deletion time does not support SQL NULL")
			}
		}
	}
	stamp := time.Now().UTC().UnixMilli()
	for _, value := range []*int64{&stamp, nil} {
		if err := db.WithContext(ctx).Model(entity).Where("id = ?", id).UpdateColumn("delete_at", value).Error; err != nil {
			t.Fatal(err)
		}
		// This is a narrow metadata projection, not a persistence entity.
		var stored struct {
			CreateAt int64
			UpdateAt int64
			DeleteAt *int64
		}
		if err := db.WithContext(ctx).Model(entity).Select([]string{"create_at", "update_at", "delete_at"}).Where("id = ?", id).Take(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.CreateAt <= 0 || stored.UpdateAt <= 0 {
			t.Fatal("generated creation/update timestamps were not populated")
		}
		if value == nil && stored.DeleteAt != nil || value != nil && (stored.DeleteAt == nil || *stored.DeleteAt != *value) {
			t.Fatal("nullable deletion time did not round-trip")
		}
	}
}
