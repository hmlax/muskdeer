package venue

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

// lockCurrentInstance is held through the final bounded SDK write, alongside
// the existing fund/source/owner locks. Zero IDs exist only in legacy test seams.
func lockCurrentInstance(ctx context.Context, tx *gorm.DB, id, revision int64, account, symbol, source string, kinds ...domain.StrategyTemplate) error {
	if id == 0 && revision == 0 {
		return nil
	}
	if id <= 0 || revision <= 0 {
		return domain.ErrLeaseLost
	}
	var row model.StrategyInstance
	if err := tx.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&model.StrategyInstance{}).
		Where("id = ? AND revision = ? AND desired_running = ? AND delete_at IS NULL", id, revision, true).
		Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
		return domain.ErrLeaseLost
	}
	if account != "" && row.AccountId != account || row.MarketDataSource != source {
		return domain.ErrLeaseLost
	}
	var template model.StrategyTemplate
	if err := tx.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&model.StrategyTemplate{}).Where("id = ? AND revision = ? AND enabled = ? AND delete_at IS NULL", row.TemplateId, row.TemplateRevision, true).Clauses(clause.Locking{Strength: "SHARE"}).Take(&template).Error; err != nil {
		return domain.ErrLeaseLost
	}
	if !strategyregistry.Matches(&template) || row.Template != template.StrategyType || row.Implementation != template.Implementation {
		return domain.ErrLeaseLost
	}
	var instrument commonmodel.Instrument
	if err := tx.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&commonmodel.Instrument{}).Where("id = ? AND symbol = ? AND status = ? AND delete_at IS NULL", row.InstrumentId, symbol, domain.InstrumentStatusTrading).Clauses(clause.Locking{Strength: "SHARE"}).Take(&instrument).Error; err != nil {
		return domain.ErrLeaseLost
	}
	for _, kind := range kinds {
		if row.Template == string(kind) {
			return nil
		}
	}
	return domain.ErrLeaseLost
}
