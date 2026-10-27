//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

func TestTypedProtoParametersRoundTripThroughGeneratedJSONBModels(t *testing.T) {
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
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		version := storedParameterFixture(template)
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			instance := identityInstance(instrument.Id, "typed-"+string(template), template, domain.MarketSourceBinance)
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
				return err
			}
			version.InstanceId = instance.Id
			return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error
		}); err != nil {
			t.Fatal(err)
		}
		var stored modelv1.StrategyConfigVersion
		if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).First(&stored, version.Id).Error; err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(version, &stored) {
			t.Fatalf("typed JSONB changed parameters for %s: %v", template, &stored)
		}
		if template == domain.StrategyTemplateMarketMaker {
			if _, err := strategyconfig.Maker(stored.MakerParameters); err != nil {
				t.Fatal(err)
			}
		} else if _, err := strategyconfig.Grid(stored.GridParameters); err != nil {
			t.Fatal(err)
		}
		assertInvalidParameterRows(ctx, t, db, version)
	}
}

func assertInvalidParameterRows(ctx context.Context, t *testing.T, db *gorm.DB, original *modelv1.StrategyConfigVersion) {
	t.Helper()
	for _, change := range []func(*modelv1.StrategyConfigVersion){
		func(row *modelv1.StrategyConfigVersion) { row.ParametersSchemaVersion = 2 },
		func(row *modelv1.StrategyConfigVersion) { row.ParametersSchemaVersion = 0 },
		func(row *modelv1.StrategyConfigVersion) {
			row.MakerParameters = nil
			row.GridParameters = nil
		},
		func(row *modelv1.StrategyConfigVersion) {
			row.MakerParameters = storedParameterFixture(domain.StrategyTemplateMarketMaker).MakerParameters
			row.GridParameters = storedParameterFixture(domain.StrategyTemplateGrid).GridParameters
		},
		func(row *modelv1.StrategyConfigVersion) {
			if row.MakerParameters != nil {
				row.Template = proto.String(string(domain.StrategyTemplateGrid))
				row.MakerParameters = nil
				row.GridParameters = storedParameterFixtureGrid()
			} else {
				row.Template = proto.String(string(domain.StrategyTemplateMarketMaker))
				row.MakerParameters = storedParameterFixture(domain.StrategyTemplateMarketMaker).MakerParameters
				row.GridParameters = nil
			}
		},
	} {
		invalid := proto.CloneOf(original)
		invalid.Id = 0
		invalid.Version = 2
		change(invalid)
		err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(invalid).Error
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" && databaseError.Code != "23503" {
			t.Fatalf("invalid parameter envelope was stored: %v", err)
		}
	}
	column := "maker_parameters"
	field := "level_notional"
	if original.GridParameters != nil {
		column = "grid_parameters"
		field = "per_order_notional"
	}
	// These are explicit stored-wire corruption fixtures, not object-to-map
	// bridges. JSONB constraints reject both unknown fields and decimal numbers.
	for _, fragment := range []string{`{"unrecognized":"invalid"}`, `{"` + field + `":1}`} {
		err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("id = ?", original.Id).
			UpdateColumn(column, gorm.Expr(column+" || ?::jsonb", fragment)).Error
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("invalid stored-wire shape was accepted: %v", err)
		}
	}
}

func storedParameterFixture(template domain.StrategyTemplate) *modelv1.StrategyConfigVersion {
	row := &modelv1.StrategyConfigVersion{
		Version:                 1,
		MarketDataSource:        string(domain.MarketSourceBinance),
		CodeVersion:             identityFixtureCode,
		CreatedBy:               identityFixtureActor,
		ParametersSchemaVersion: domain.ParametersSchemaVersion,
		Template:                proto.String(string(template)),
	}
	if template == domain.StrategyTemplateMarketMaker {
		row.MakerParameters = &commonv1.MakerParameters{
			QuoteLevels:          2,
			LevelSpacingBps:      "2.0000000001",
			BaseSpreadBps:        "10.0000000001",
			LevelNotional:        "10.000000000000000001",
			SideBudget:           "20.000000000000000002",
			InventoryTarget:      "0",
			InventorySoftLimit:   "1",
			InventoryHardLimit:   "2",
			RepriceBps:           "1",
			MinRefreshIntervalMs: 100,
			MaxQuoteAgeMs:        1000,
			MaxMarketAgeMs:       500,
			HedgeThreshold:       "0",
			MaxHedgeBacklog:      "0",
		}
	} else {
		row.GridParameters = storedParameterFixtureGrid()
	}
	return row
}

func storedParameterFixtureGrid() *commonv1.GridParameters {
	return &commonv1.GridParameters{
		LowerPrice:       "100.000000000000000001",
		UpperPrice:       "200.000000000000000002",
		GridCount:        3,
		TotalBudget:      "30.000000000000000003",
		PerOrderNotional: "10.000000000000000001",
		InventoryLimit:   "1",
		MaxMarketAgeMs:   500,
	}
}
