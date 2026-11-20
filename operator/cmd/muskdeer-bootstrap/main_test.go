package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	_ "modernc.org/sqlite"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
)

func bootstrapFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.New(sqlite.Config{
		DriverName: "sqlite",
		DSN:        ":memory:",
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	return db
}
func TestBootstrapCreatesEmptySchemaAndRefusesExistingDatabase(t *testing.T) {
	db := bootstrapFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	require.NoError(t, bootstrapDatabase(ctx, db))
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tables), len(store.AllModels()))
	name := "existing-admin"
	require.NoError(t, db.WithContext(ctx).Model(&store.User{}).Create(&store.User{Username: &name}).Error)
	require.ErrorContains(t, bootstrapDatabase(ctx, db), "empty administration database")
	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&store.User{}).Where("username = ?", name).Count(&count).Error)
	require.Equal(t, int64(1), count, "rejected bootstrap must retain existing users")
	require.True(t, db.WithContext(ctx).Migrator().HasIndex(&store.MuskdeerAccountBinding{}, "uk_muskdeer_binding_account"))
}
