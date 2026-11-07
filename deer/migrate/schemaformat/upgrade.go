package schemaformat

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

//go:embed 0002_unlimited_synthetic.sql
var unlimitedDefinition string

//go:embed 0003_console_instance_identity.sql
var consoleDefinition string

const consoleName = "0003_console_instance_identity.sql"

func consoleChecksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(consoleDefinition))) }
func matchUnlimited(rows []*model.SchemaMigration) bool {
	return len(rows) == 2 && matchBaseline(rows[0]) && rows[1].Version == 2 && rows[1].Name == unlimitedName && rows[1].Checksum == unlimitedChecksum() && rows[1].DeleteAt == nil
}

const unlimitedName = "0002_unlimited_synthetic.sql"

func unlimitedChecksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(unlimitedDefinition))) }

// Upgrade is explicit offline maintenance. It retains the immutable baseline
// and every consumed slot, and never runs from service startup.
func Upgrade(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil || ctx.Err() != nil {
		return ErrHistory
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrHistory
	}
	return schemaDB(ctx, db).Model(&model.SchemaMigration{}).Transaction(func(tx *gorm.DB) error {
		if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(0x4d55534b), int32(0x494e5354)).Error; err != nil {
			return err
		}
		var rows []*model.SchemaMigration
		if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Order("version").Limit(4).Find(&rows).Error; err != nil {
			return err
		}
		if Match(rows) {
			return nil
		}
		if !matchUnlimited(rows) && (len(rows) != 1 || !matchBaseline(rows[0])) {
			return ErrHistory
		}
		var active int64
		if err := schemaDB(ctx, tx).Model(&model.ExecutionShardLease{}).
			Where("delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return domain.ErrLeaseHeld
		}

		if len(rows) == 1 {
			if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Exec(unlimitedDefinition).Error; err != nil {
				return err
			}
			if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
				Version:  2,
				Name:     unlimitedName,
				Checksum: unlimitedChecksum(),
			}).Error; err != nil {
				return err
			}
		}
		if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Exec(consoleDefinition).Error; err != nil {
			return err
		}
		return schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
			Version:  3,
			Name:     consoleName,
			Checksum: consoleChecksum(),
		}).Error
	})
}

func schemaDB(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}
