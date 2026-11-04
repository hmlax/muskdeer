//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	marketmodelv1 "github.com/hmlax/muskdeer/deer/proto/market/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/proto/portfolio/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/proto/strategy/model/v1"
)

func TestEveryMigratedModelHasRequiredCommonDatabaseColumns(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, entity := range []any{
		&commonmodelv1.SchemaMigration{},
		&commonmodelv1.AdminUser{},
		&commonmodelv1.AdminSession{},
		&commonmodelv1.AdminAudit{},
		&strategymodelv1.AdminStrategyOperation{},
		&strategymodelv1.AdminStrategyCreation{},
		&strategymodelv1.AdminStrategyPublication{},
		&strategymodelv1.AdminStrategyRiskChange{},
		&executionmodelv1.AdminOrderCancellation{},
		&executionmodelv1.AdminEmergencyCancellation{},
		&executionmodelv1.AdminEmergencyCancellationTask{},
		&executionmodelv1.AdminAccountFlattening{},
		&executionmodelv1.AdminAccountFlatteningTask{},
		&commonmodelv1.Instrument{},
		&strategymodelv1.StrategyInstance{},
		&strategymodelv1.StrategyConfigVersion{},
		&strategymodelv1.StrategyAction{},
		&strategymodelv1.StrategyLease{},
		&commonmodelv1.ExecutionShardLease{},
		&executionmodelv1.SimulatorCommand{},
		&executionmodelv1.SimulatorOrder{},
		&executionmodelv1.SimulatorAccount{},
		&executionmodelv1.SimulatorBalance{},
		&executionmodelv1.SimulatorFill{},
		&commonmodelv1.AccountBudget{},
		&strategymodelv1.StrategyBudget{},
		&executionmodelv1.RiskSnapshot{},
		&executionmodelv1.RiskSnapshotEvent{},
		&commonmodelv1.RiskBudget{},
		&executionmodelv1.OrderIntent{},
		&executionmodelv1.OutboxEvent{},
		&executionmodelv1.CancellationRequest{},
		&portfoliomodelv1.PortfolioAccount{},
		&portfoliomodelv1.PortfolioFill{},
		&portfoliomodelv1.ConsumerInbox{},
		&marketmodelv1.MarketFeed{},
		&marketmodelv1.MarketPublisherLease{},
		&marketmodelv1.MarketSnapshot{},
	} {
		columns, err := db.WithContext(ctx).Model(entity).Migrator().ColumnTypes(entity)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, column := range columns {
			switch column.Name() {
			case "id":
				found++
				if primary, known := column.PrimaryKey(); !known || !primary {
					t.Fatalf("%T lacks a real primary key", entity)
				}
			case "create_at", "update_at", "delete_at":
				found++
				nullable, known := column.Nullable()
				if !known || nullable != (column.Name() == "delete_at") {
					t.Fatalf("%T %s NULL constraint is wrong", entity, column.Name())
				}
			}
		}
		if found != 4 {
			t.Fatalf("%T exposes %d common database columns, expected 4", entity, found)
		}
	}
}
