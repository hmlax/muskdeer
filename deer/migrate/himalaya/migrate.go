// Package himalayamigrate owns explicit versioned SDK extension migrations.
// Application startup only calls Check; it never performs DDL.
package himalayamigrate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

//go:embed sql/0001_sdk_intents.sql
var definition string

//go:embed sql/0002_live_execution.sql
var liveDefinition string

// ErrHistory rejects missing, changed or future SDK schema history.
var ErrHistory = errors.New("himalaya SDK schema history differs from this binary")

// Apply installs the extension explicitly, preserving the core migration ledger.
func Apply(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrHistory
	}
	return db.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).
			Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(0x4d55534b), int32(0x48494d41)).Error; err != nil {
			return err
		}
		var applied []*modelv1.HimalayaSchemaMigration
		if tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Migrator().HasTable(&modelv1.HimalayaSchemaMigration{}) {
			if err := tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Order("version").Find(&applied).Error; err != nil {
				return err
			}
			if len(applied) == 0 || validate(applied, false) != nil {
				return ErrHistory
			}
		}
		for index := len(applied); index < len(definitions()); index++ {
			current := definitions()[index]
			if err := tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Exec(current).Error; err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Create(&modelv1.HimalayaSchemaMigration{
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
	var rows []*modelv1.HimalayaSchemaMigration
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Order("version").Find(&rows).Error; err != nil {
		return err
	}
	return validate(rows, true)
}

func validate(rows []*modelv1.HimalayaSchemaMigration, complete bool) error {
	if len(rows) > len(definitions()) || complete && len(rows) != len(definitions()) {
		return ErrHistory
	}
	for index, row := range rows {
		if row.Version != int64(index+1) || row.Checksum != checksum(definitions()[index]) || row.DeleteAt != nil {
			return ErrHistory
		}
	}
	return nil
}

func definitions() []string { return []string{definition, liveDefinition} }

func checksum(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
