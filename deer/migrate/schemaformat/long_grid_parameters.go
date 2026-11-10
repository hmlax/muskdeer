package schemaformat

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"gorm.io/gorm"
)

//go:embed 0007_long_grid_parameters.sql
var longGridParametersDefinition string

const longGridParametersName = "0007_long_grid_parameters.sql"

func longGridParametersChecksum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(longGridParametersDefinition)))
}
func matchLongGrid(rows []*model.SchemaMigration) bool {
	return len(rows) == 6 && matchMatching(rows[:5]) && rows[5].Version == 6 && rows[5].Name == longGridName && rows[5].Checksum == longGridChecksum() && rows[5].DeleteAt == nil
}
func upgradeLongGridParameters(ctx context.Context, tx *gorm.DB) error {
	if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Exec(longGridParametersDefinition).Error; err != nil {
		return err
	}
	return schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
		Version:  7,
		Name:     longGridParametersName,
		Checksum: longGridParametersChecksum(),
	}).Error
}
