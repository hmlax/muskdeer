package instancemove

import (
	"context"
	"sync"

	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/model/v1"
	portfolio "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func unusedModels() []any {
	return []any{
		&common.AccountBudget{},
		&common.RiskBudget{},
		&common.ServiceRuntimePolicy{},
		&common.AdminUser{},
		&common.AdminSession{},
		&common.AdminAudit{},
		&market.MarketFeed{},
		&market.MarketPublisherLease{},
		&market.MarketSnapshot{},
		&strategy.StrategyLease{},
		&strategy.StrategyBudget{},
		&strategy.StrategyAction{},
		&strategy.AdminStrategyOperation{},
		&strategy.AdminStrategyCreation{},
		&strategy.AdminStrategyPublication{},
		&strategy.AdminStrategyRiskChange{},
		&execution.SimulatorAccount{},
		&execution.SimulatorBalance{},
		&execution.SimulatorCommand{},
		&execution.SimulatorFill{},
		&execution.SimulatorOrder{},
		&execution.OrderIntent{},
		&execution.CancellationRequest{},
		&execution.RiskSnapshot{},
		&execution.RiskSnapshotEvent{},
		&execution.OutboxEvent{},
		&execution.AdminOrderCancellation{},
		&execution.AdminEmergencyCancellation{},
		&execution.AdminEmergencyCancellationTask{},
		&execution.AdminAccountFlattening{},
		&execution.AdminAccountFlatteningTask{},
		&portfolio.PortfolioAccount{},
		&portfolio.PortfolioFill{},
		&portfolio.ConsumerInbox{},
	}
}
func checkSource(ctx context.Context, db *gorm.DB) error {
	var active int64
	if err := fresh(ctx, db).Model(&common.ExecutionShardLease{}).Where("expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AND delete_at IS NULL").Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return ErrSource
	}
	known := make(map[string]bool)
	models := append(keptModels(), unusedModels()...)
	models = append(models, &common.VenueSchemaMigration{}, &common.RuntimePolicyMigration{})
	for _, entity := range models {
		parsed, err := schema.Parse(entity, &sync.Map{}, db.NamingStrategy)
		if err != nil {
			return err
		}
		known[parsed.Table] = true
	}
	existing, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return err
	}
	for _, name := range existing {
		if !known[name] {
			return ErrSource
		}
	}
	for _, entity := range unusedModels() {
		if !db.WithContext(ctx).Migrator().HasTable(entity) {
			continue
		}
		var count int64
		if err := fresh(ctx, db).Model(entity).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrSource
		}
	}
	return nil
}
func copyHistory(ctx context.Context, src, dst *gorm.DB) error {
	var core []*common.SchemaMigration
	if err := fresh(ctx, src).Model(&common.SchemaMigration{}).Order("id").Find(&core).Error; err != nil {
		return err
	}
	var venue []*common.VenueSchemaMigration
	if err := fresh(ctx, src).Model(&common.VenueSchemaMigration{}).Order("id").Find(&venue).Error; err != nil {
		return err
	}
	var policies []*common.RuntimePolicyMigration
	if src.WithContext(ctx).Migrator().HasTable(&common.RuntimePolicyMigration{}) {
		if err := fresh(ctx, src).Model(&common.RuntimePolicyMigration{}).Order("id").Find(&policies).Error; err != nil {
			return err
		}
	}
	var history []*common.MigrationHistory
	for _, row := range core {
		history = append(history, &common.MigrationHistory{
			CreateAt:   row.CreateAt,
			UpdateAt:   row.UpdateAt,
			DeleteAt:   row.DeleteAt,
			Source:     sourceCore,
			OriginalId: row.Id,
			Version:    row.Version,
			Name:       row.Name,
			Checksum:   row.Checksum,
		})
	}
	for _, row := range venue {
		history = append(history, &common.MigrationHistory{
			CreateAt:   row.CreateAt,
			UpdateAt:   row.UpdateAt,
			DeleteAt:   row.DeleteAt,
			Source:     sourceVenue,
			OriginalId: row.Id,
			Version:    row.Version,
			Checksum:   row.Checksum,
		})
	}
	for _, row := range policies {
		history = append(history, &common.MigrationHistory{
			CreateAt:   row.CreateAt,
			UpdateAt:   row.UpdateAt,
			DeleteAt:   row.DeleteAt,
			Source:     sourceRuntimePolicy,
			OriginalId: row.Id,
			Version:    row.Version,
			Checksum:   row.Checksum,
		})
	}
	return fresh(ctx, dst).Model(&common.MigrationHistory{}).CreateInBatches(history, 128).Error
}
