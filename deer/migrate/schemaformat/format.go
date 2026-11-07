// Package schemaformat defines the consolidated, explicitly installed runtime
// schema. Startup only checks its immutable ledger; it never executes DDL.
package schemaformat

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	strategymodel "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"gorm.io/gorm"
)

//go:embed 0001_strategy_instances.sql
var definition string

const baselineName = "0001_strategy_instances.sql"

// ErrHistory rejects incomplete, altered or legacy schema ledgers.
var ErrHistory = errors.New("consolidated strategy instance schema is required")

func checksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(definition))) }

// Match checks the complete consolidated ledger identity.
func Match(rows []*model.SchemaMigration) bool {
	return len(rows) == 1 && rows[0].Version == 1 && rows[0].Name == baselineName && rows[0].Checksum == checksum() && rows[0].DeleteAt == nil
}

// Check validates the current ledger without changing database objects.
func Check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrHistory
	}
	var rows []*model.SchemaMigration
	if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Order("version").Limit(2).Find(&rows).Error; err != nil {
		return errors.Join(ErrHistory, err)
	}
	if !Match(rows) {
		return ErrHistory
	}
	return nil
}

// Install is maintenance-only and requires an empty schema. No IF NOT EXISTS,
// AutoMigrate, checksum rewriting, old migration replay, or silent repair.
func Install(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrHistory
	}
	return db.WithContext(ctx).Model(&model.SchemaMigration{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&model.SchemaMigration{}).Exec(definition).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{
			NewDB:   true,
			Context: ctx,
		}).Model(&strategymodel.StrategyTemplate{}).Create(strategyregistry.Templates()).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
			Version:  1,
			Name:     baselineName,
			Checksum: checksum(),
		}).Error
	})
}
