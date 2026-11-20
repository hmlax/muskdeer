// Package storetest supplies isolated GORM databases for repository/service tests.
package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	// Register the isolated SQLite fixture driver.
	_ "modernc.org/sqlite"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
)

func NewClientForTest(t *testing.T) *store.DBClient {
	t.Helper()
	db, err := gorm.Open(sqlite.New(sqlite.Config{
		DriverName: "sqlite",
		DSN:        fmt.Sprintf("file:storetest_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", time.Now().UnixNano()),
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	client := store.NewClient(db)
	require.NoError(t, client.Schema.Create(context.Background()), "GORM fixture migration failed")
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	return store.NewDBClient(client, pool)
}

func NewSystemContext(ctx context.Context) context.Context { return viewer.WithSystemContext(ctx) }
