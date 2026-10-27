//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

func TestEveryMigratedModelHasRequiredCommonDatabaseColumns(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, entity := range []any{
		&modelv1.SchemaMigration{},
		&modelv1.Instrument{},
		&modelv1.StrategyInstance{},
		&modelv1.StrategyConfigVersion{},
		&modelv1.StrategyLease{},
		&modelv1.ExecutionShardLease{},
		&modelv1.SimulatorCommand{},
		&modelv1.SimulatorOrder{},
		&modelv1.SimulatorAccount{},
		&modelv1.SimulatorBalance{},
		&modelv1.SimulatorFill{},
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
