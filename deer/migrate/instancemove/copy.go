// Package instancemove copies a stopped legacy native database into an empty
// consolidated schema. It never changes the source or calls an exchange.
package instancemove

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/hmlax/muskdeer/deer/migrate"
	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	portfolio "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

const (
	sourceCore          = "core"
	sourceVenue         = "venue"
	sourceRuntimePolicy = "runtime_policy"
)

// ErrSource prevents active-source copying and loss of unsupported nonempty data.
var ErrSource = errors.New("legacy migration source is active, unsupported or inconsistent")

func fresh(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}

// Move commits baseline, copied facts, transformed instances and verification
// together in the target. A failure leaves the empty target empty. The caller
// must stop all writers first; unexpired durable leases are also rejected.
func Move(ctx context.Context, source, target *gorm.DB) error {
	if ctx == nil || source == nil || target == nil || source == target {
		return ErrSource
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrSource
	}
	return fresh(ctx, source).Transaction(func(src *gorm.DB) error {
		if schemaformat.Check(ctx, src) == nil {
			return ErrSource
		}
		if err := migrate.CheckCurrent(ctx, src); err != nil {
			return err
		}
		if err := venuemigrate.Check(ctx, src); err != nil {
			return err
		}
		if err := checkSource(ctx, src); err != nil {
			return err
		}
		converted, extraVersions, err := convertInstances(ctx, src)
		if err != nil {
			return err
		}
		return fresh(ctx, target).Transaction(func(dst *gorm.DB) error {
			if err := schemaformat.Install(ctx, dst); err != nil {
				return err
			}
			if err := copyFacts(ctx, src, dst, converted, extraVersions); err != nil {
				return err
			}
			if err := schemaformat.Check(ctx, dst); err != nil {
				return err
			}
			loaded, err := instanceconfig.Load(ctx, dst)
			if err != nil {
				return err
			}
			expected := 0
			for _, row := range converted {
				if row.DesiredRunning && row.DeleteAt == nil {
					expected++
				}
			}
			if len(loaded) != expected {
				return ErrSource
			}
			return nil
		})
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
}

func copyRows[T any](ctx context.Context, source, target *gorm.DB, entity *T, transform func(*T) error) error {
	var rows []*T
	var count int64
	err := fresh(ctx, source).Model(entity).Order("id").FindInBatches(&rows, 256, func(_ *gorm.DB, _ int) error {
		ids := make([]int64, 0, len(rows))
		expected := make([]proto.Message, 0, len(rows))
		for _, row := range rows {
			if transform != nil {
				if err := transform(row); err != nil {
					return err
				}
			}
			ids = append(ids, any(row).(interface{ GetId() int64 }).GetId())
			expected = append(expected, proto.Clone(any(row).(proto.Message)))
		}
		if err := fresh(ctx, target).Model(entity).CreateInBatches(rows, 256).Error; err != nil {
			return err
		}
		var copied []*T
		if err := fresh(ctx, target).Model(entity).Where("id IN ?", ids).Order("id").Find(&copied).Error; err != nil {
			return err
		}
		if len(copied) != len(rows) {
			return ErrSource
		}
		for i, row := range expected {
			if !proto.Equal(row, any(copied[i]).(proto.Message)) {
				return fmt.Errorf("%w: copied %s differs at id %d", ErrSource, reflect.TypeFor[T]().Name(), ids[i])
			}
		}
		count += int64(len(rows))
		return nil
	}).Error
	if err != nil {
		return err
	}
	var actual int64
	if err := fresh(ctx, target).Model(entity).Count(&actual).Error; err != nil {
		return err
	}
	if actual != count {
		return ErrSource
	}
	return nil
}

func resetSequence(ctx context.Context, source, db *gorm.DB, entity any) error {
	parsed, err := schema.Parse(entity, &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return err
	}
	var maximum sql.NullInt64
	if err := fresh(ctx, db).Model(entity).Select("max(id)").Scan(&maximum).Error; err != nil {
		return err
	}
	name := pgx.Identifier{parsed.Table}.Sanitize()
	highWater, err := sequenceHighWater(ctx, source, entity)
	if err != nil {
		return err
	}
	if highWater.Valid && highWater.Int64 > maximum.Int64 {
		maximum = highWater
	}
	return fresh(ctx, db).Model(entity).Exec("SELECT setval(pg_get_serial_sequence(?, 'id'), ?, ?)", name, max(maximum.Int64, 1), maximum.Valid).Error
}
func keptModels() []any {
	return []any{
		&common.SchemaMigration{},
		&common.MigrationHistory{},
		&common.ExecutionShardLease{},
		&common.Instrument{},
		&common.TradingAccountPolicy{},
		&strategy.StrategyInstance{},
		&strategy.StrategyConfigVersion{},
		&execution.VenueOrderIntent{},
		&execution.VenueCancellation{},
		&execution.SyntheticTapeIntent{},
		&execution.ExecutionRecoveryBaseline{},
		&portfolio.VenueAccountSnapshot{},
	}
}

func copyFacts(ctx context.Context, src, dst *gorm.DB, converted []*strategy.StrategyInstance, extraVersions []*strategy.StrategyConfigVersion) error {
	if err := copyRows(ctx, src, dst, &common.ExecutionShardLease{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &common.Instrument{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &common.TradingAccountPolicy{}, nil); err != nil {
		return err
	}
	if err := fresh(ctx, dst).Model(&strategy.StrategyInstance{}).CreateInBatches(converted, 128).Error; err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &strategy.StrategyConfigVersion{}, nil); err != nil {
		return err
	}
	if len(extraVersions) > 0 {
		if err := fresh(ctx, dst).Model(&strategy.StrategyConfigVersion{}).CreateInBatches(extraVersions, 128).Error; err != nil {
			return err
		}
	}
	if err := copyRows(ctx, src, dst, &execution.VenueOrderIntent{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &execution.VenueCancellation{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &execution.SyntheticTapeIntent{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &execution.ExecutionRecoveryBaseline{}, nil); err != nil {
		return err
	}
	if err := copyRows(ctx, src, dst, &portfolio.VenueAccountSnapshot{}, func(row *portfolio.VenueAccountSnapshot) error {
		if row.Snapshot.GetScope().GetSymbol() == "" {
			return ErrSource
		}
		row.Symbol = row.Snapshot.Scope.Symbol
		return nil
	}); err != nil {
		return err
	}
	if err := copyHistory(ctx, src, dst); err != nil {
		return err
	}
	for _, entity := range keptModels() {
		if err := resetSequence(ctx, src, dst, entity); err != nil {
			return err
		}
	}
	return nil
}

func sequenceHighWater(ctx context.Context, db *gorm.DB, entity any) (sql.NullInt64, error) {
	var value sql.NullInt64
	if !db.WithContext(ctx).Migrator().HasTable(entity) {
		return value, nil
	}
	parsed, err := schema.Parse(entity, &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return value, err
	}
	name := pgx.Identifier{parsed.Table}.Sanitize()
	err = fresh(ctx, db).Model(entity).Raw("SELECT pg_sequence_last_value(pg_get_serial_sequence(?, 'id')::regclass)", name).Scan(&value).Error
	return value, err
}
