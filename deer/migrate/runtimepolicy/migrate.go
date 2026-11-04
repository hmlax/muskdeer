// Package runtimepolicy owns explicit, checksummed business-configuration migrations.
package runtimepolicy

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
)

//go:embed sql/0001_service_runtime_policies.sql
var definition string

const messageHistory = "runtime policy schema history differs from this binary"

// ErrHistory rejects missing, changed or unsupported configuration schema history.
var ErrHistory = errors.New(messageHistory)

// Apply is an explicit maintenance operation; startup only calls Check.
func Apply(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrHistory
	}
	return db.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(0x4d55534b), int32(0x52504346)).Error; err != nil {
			return err
		}
		if tx.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Migrator().HasTable(&model.RuntimePolicyMigration{}) {
			return Check(ctx, tx)
		}
		if err := tx.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Exec(definition).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Create(&model.RuntimePolicyMigration{
			Version:  1,
			Checksum: checksum(),
		}).Error
	})
}

// Check validates immutable history without writing schema or policy rows.
func Check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrHistory
	}
	var rows []*model.RuntimePolicyMigration
	if err := db.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Order("version").Find(&rows).Error; err != nil {
		return errors.Join(ErrHistory, err)
	}
	if len(rows) != 1 || rows[0].Version != 1 || rows[0].Checksum != checksum() || rows[0].DeleteAt != nil {
		return ErrHistory
	}
	return nil
}

func checksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(definition))) }
