// Package venuemigrate owns explicit versioned venue execution migrations.
// Application startup only calls Check; it never performs DDL.
package venuemigrate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"

	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
)

//go:embed sql/0001_sdk_intents.sql
var definition string

//go:embed sql/0002_live_execution.sql
var liveDefinition string

//go:embed sql/0003_synthetic_tape.sql
var syntheticDefinition string

//go:embed sql/0004_development_baseline.sql
var developmentBaselineDefinition string

//go:embed sql/0005_historical_intent_approval.sql
var historicalIntentDefinition string

//go:embed sql/0006_database_profiles.sql
var databaseProfilesDefinition string

//go:embed sql/0007_venue_business_models.sql
var venueModelsDefinition string

//go:embed sql/0008_versioned_strategy_parameters.sql
var strategyParametersDefinition string

// Historical checksum definitions only. New installations never execute the
// stateless DROP migration; this branch is selected only after it is recorded.
//
//go:embed legacy/0007_stateless_orders.sql
var legacyStatelessDefinition string

//go:embed legacy/0008_plan_grace.sql
var legacyPlanGraceDefinition string

//go:embed sql/0009_stateless_business_audits.sql
var statelessBusinessDefinition string

// Both legacy and current installers retain the same advisory lock. A ledger
// rename and all business table changes commit in one bounded transaction.
const bootstrapLedger = `DO $$
BEGIN
  IF to_regclass('venue_schema_migrations') IS NOT NULL AND to_regclass('himalaya_schema_migrations') IS NOT NULL THEN
    RAISE EXCEPTION 'ambiguous venue migration ledgers';
  END IF;
  IF to_regclass('venue_schema_migrations') IS NULL AND to_regclass('himalaya_schema_migrations') IS NOT NULL THEN
    ALTER TABLE himalaya_schema_migrations RENAME TO venue_schema_migrations;
  END IF;
END $$;`

// ErrHistory rejects missing, changed or future SDK schema history.
var ErrHistory = errors.New("venue execution schema history differs from this binary")

// Apply installs the extension explicitly, preserving the core migration ledger.
func Apply(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrHistory
	}
	return db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).
			Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(0x4d55534b), int32(0x48494d41)).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec(bootstrapLedger).Error; err != nil {
			return err
		}
		var applied []*commonmodelv1.VenueSchemaMigration
		if tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Migrator().HasTable(&commonmodelv1.VenueSchemaMigration{}) {
			if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Order("version").Find(&applied).Error; err != nil {
				return err
			}
			if len(applied) == 0 || validate(applied, false) != nil {
				return ErrHistory
			}
		}
		path := definitionsFor(applied)
		for index := len(applied); index < len(path); index++ {
			current := path[index]
			if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec(current).Error; err != nil {
				return err
			}
			if index == 0 {
				if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Exec(bootstrapLedger).Error; err != nil {
					return err
				}
			}
			if err := tx.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Create(&commonmodelv1.VenueSchemaMigration{
				Version:  int64(index + 1),
				Checksum: checksum(current),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Check verifies the immutable extension history without creating any objects.
func Check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if schemaformat.Check(ctx, db) == nil {
		return nil
	}
	var rows []*commonmodelv1.VenueSchemaMigration
	if err := db.WithContext(ctx).Model(&commonmodelv1.VenueSchemaMigration{}).Order("version").Find(&rows).Error; err != nil {
		return errors.Join(ErrHistory, err)
	}
	return validate(rows, true)
}

func validate(rows []*commonmodelv1.VenueSchemaMigration, complete bool) error {
	path := definitionsFor(rows)
	if len(rows) > len(path) || complete && len(rows) != len(path) {
		return ErrHistory
	}
	for index, row := range rows {
		if row.Version != int64(index+1) || row.Checksum != checksum(path[index]) || row.DeleteAt != nil {
			return ErrHistory
		}
	}
	return nil
}

func definitions() []string {
	return []string{
		definition,
		liveDefinition,
		syntheticDefinition,
		developmentBaselineDefinition,
		historicalIntentDefinition,
		databaseProfilesDefinition,
		venueModelsDefinition,
		strategyParametersDefinition,
	}
}

func definitionsFor(rows []*commonmodelv1.VenueSchemaMigration) []string {
	if len(rows) >= 7 && rows[6].Checksum == checksum(legacyStatelessDefinition) {
		// At least seven versions already exist, so the loop can never execute
		// legacyStatelessDefinition. Retain its identity and append safe upgrades.
		return []string{
			definition,
			liveDefinition,
			syntheticDefinition,
			developmentBaselineDefinition,
			historicalIntentDefinition,
			databaseProfilesDefinition,
			legacyStatelessDefinition,
			legacyPlanGraceDefinition,
			statelessBusinessDefinition,
			strategyParametersDefinition,
		}
	}
	return definitions()
}

func checksum(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
