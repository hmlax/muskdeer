package schemaformat

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"gorm.io/gorm"
)

//go:embed 0006_long_grid.sql
var longGridDefinition string

const longGridName = "0006_long_grid.sql"

func longGridChecksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(longGridDefinition))) }
func matchMatching(rows []*model.SchemaMigration) bool {
	return len(rows) == 5 && matchReservation(rows[:4]) && rows[4].Version == 5 && rows[4].Name == matchingName && rows[4].Checksum == matchingChecksum() && rows[4].DeleteAt == nil
}
func upgradeLongGrid(ctx context.Context, tx *gorm.DB) error {
	if err := schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Exec(longGridDefinition).Error; err != nil {
		return err
	}
	var previous strategy.StrategyTemplate
	err := schemaDB(ctx, tx).Model(&strategy.StrategyTemplate{}).Where("id = ?", strategyregistry.LongGridTemplateID).Take(&previous).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		for _, seed := range strategyregistry.Templates() {
			if seed.Id == strategyregistry.LongGridTemplateID {
				if err := schemaDB(ctx, tx).Model(&strategy.StrategyTemplate{}).Create(seed).Error; err != nil {
					return err
				}
			}
		}
	case err != nil:
		return err
	case previous.Implementation != strategyregistry.ImplementationLongGrid || !strategyregistry.Matches(&previous):
		return ErrHistory
	}
	return schemaDB(ctx, tx).Model(&model.SchemaMigration{}).Create(&model.SchemaMigration{
		Version:  6,
		Name:     longGridName,
		Checksum: longGridChecksum(),
	}).Error
}
