package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func fixture(t *testing.T) *Client {
	t.Helper()
	db, err := gorm.Open(sqlite.New(sqlite.Config{
		DriverName: "sqlite",
		DSN:        fmt.Sprintf("file:gorm_%d?mode=memory&cache=shared", time.Now().UnixNano()),
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		TranslateError: true,
	})
	require.NoError(t, err)
	client := NewClient(db)
	require.NoError(t, db.Migrator().CreateTable(&User{}, &Menu{}, &Role{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return client
}

func TestPostgresAggregateAliases(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 port=1 user=unused dbname=unused sslmode=disable"), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		NamingStrategy:       schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	client := NewClient(db)
	ctx := viewer.WithSystemContext(context.Background())
	query := client.User.Query().GroupBy("tenant_id").Aggregate(As(Count(), "count"))
	statement := query.db(ctx).Find(&[]struct {
		TenantID uint32
		Count    int64
	}{}).Statement.SQL.String()
	require.Contains(t, statement, `COUNT(*) AS "count"`)
	require.NotContains(t, statement, "`", "PostgreSQL 聚合 SQL 不允许混入 MySQL 的反引号")
}

func TestTenantIsolation(t *testing.T) {
	client := fixture(t)
	ctx := viewer.WithSystemContext(context.Background())
	one, err := client.User.Create().SetUsername("same").SetTenantID(101).Save(ctx)
	require.NoError(t, err)
	two, err := client.User.Create().SetUsername("same").SetTenantID(102).Save(ctx)
	require.NoError(t, err)
	tenant := viewer.WithContext(context.Background(), viewer.NewUserContext(1, 101, 0, "test", []viewer.DataScope{{ScopeType: viewer.ScopeTypeAll}}))
	rows, err := client.User.Query().All(tenant)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, one.ID, rows[0].ID)
	_, err = client.User.Get(tenant, two.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.True(t, kratoserrors.IsNotFound(err), "missing and foreign tenant rows must serialize as 404")
	n, err := client.User.Update().Where(idEquals(two.ID)).SetNickname("cross").Save(tenant)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = client.User.Delete().Where(idEquals(two.ID)).Exec(tenant)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = client.User.UpdateOneID(one.ID).SetTenantID(102).Save(tenant)
	require.ErrorIs(t, err, ErrCrossTenant)
	_, err = client.User.Query().All(context.Background())
	require.ErrorIs(t, err, ErrMissingViewer)
}

func TestTransactionAndFalseValue(t *testing.T) {
	client := fixture(t)
	ctx := viewer.WithSystemContext(context.Background())
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	_, err = tx.User.Create().SetUsername("rolled-back").SetTenantID(101).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	n, err := client.User.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	app, err := client.Role.Create().SetName("disabled").SetTenantID(101).SetCode("disabled").SetIsProtected(false).Save(ctx)
	require.NoError(t, err)
	loaded, err := client.Role.Get(ctx, app.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.IsProtected)
	require.False(t, *loaded.IsProtected)
}

func TestTypedJSONUpdate(t *testing.T) {
	client := fixture(t)
	ctx := viewer.WithSystemContext(context.Background())
	menu, err := client.Menu.Create().SetName("model-test").SetMeta(&operatorV1.MenuMeta{Title: func() *string { v := "before"; return &v }()}).Save(ctx)
	require.NoError(t, err)
	_, err = client.Menu.UpdateOneID(menu.ID).SetMeta(&operatorV1.MenuMeta{Title: func() *string { v := "after"; return &v }()}).Save(ctx)
	require.NoError(t, err)
	loaded, err := client.Menu.Get(ctx, menu.ID)
	require.NoError(t, err)
	require.Equal(t, "after", loaded.Meta.GetTitle())
}
