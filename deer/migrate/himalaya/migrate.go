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
		if tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Migrator().HasTable(&modelv1.HimalayaSchemaMigration{}) {
			return Check(ctx, tx)
		}
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Exec(definition).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Create(&modelv1.HimalayaSchemaMigration{
			Version:  1,
			Checksum: checksum(),
		}).Error
	})
}

// Check verifies the immutable extension history without creating any objects.
func Check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	var rows []*modelv1.HimalayaSchemaMigration
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaSchemaMigration{}).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 1 || rows[0].Version != 1 || rows[0].Checksum != checksum() || rows[0].DeleteAt != nil {
		return ErrHistory
	}
	return nil
}

func checksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(definition))) }
