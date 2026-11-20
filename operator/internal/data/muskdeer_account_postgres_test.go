//go:build integration

package data

import (
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func TestMuskdeerPostgresEnrollmentSerializesSDKUserOwnership(t *testing.T) {
	dsn := os.Getenv("MUSKDEER_ACCOUNT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN required")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimPrefix(parsed.Path, "/"), "muskdeer_test_"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(4)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	ctx := storetest.NewSystemContext(t.Context())
	require.NoError(t, db.WithContext(ctx).Model(&store.MuskdeerAccountBinding{}).Migrator().CreateTable(&store.Tenant{}, &store.PermissionGroup{}, &store.MuskdeerAccountBinding{}))
	client := store.NewClient(db)
	resource := store.NewDBClient(client, pool)
	_, err = client.PermissionGroup.Create().SetName("market").SetModule(muskdeer.Module).Save(ctx)
	require.NoError(t, err)
	for _, id := range []uint32{7, 8} {
		_, err := client.Tenant.Create().SetID(id).SetName(string(rune('a' + id))).SetCode(string(rune('a' + id))).Save(ctx)
		require.NoError(t, err)
	}
	repo := NewMuskdeerBindingRepo(resource)
	platform := bindingContext(t.Context(), 1, 0)
	type outcome struct {
		tenant uint32
		err    error
	}
	results := make(chan outcome, 2)
	var group sync.WaitGroup
	for _, tenant := range []uint32{7, 8} {
		group.Go(func() {
			_, err := repo.Create(platform, &pb.MuskdeerBindingDraft{
				TenantId:  tenant,
				AccountId: muskdeer.AccountScope(100, int64(tenant)),
				Enabled:   true,
			})
			results <- outcome{
				tenant: tenant,
				err:    err,
			}
		})
	}
	group.Wait()
	close(results)
	var winner uint32
	denied := 0
	for result := range results {
		if result.err == nil {
			require.Zero(t, winner)
			winner = result.tenant
		} else {
			require.Equal(t, codes.PermissionDenied, status.Code(result.err))
			denied++
		}
	}
	require.NotZero(t, winner)
	require.Equal(t, 1, denied)
	merchant := bindingContext(t.Context(), 2, winner)
	bindings := make(chan *pb.MuskdeerAccountBinding, 2)
	errors := make(chan error, 2)
	for range 2 {
		group.Go(func() { row, err := repo.BeginAccountEnrollment(merchant, 100, 200); bindings <- row; errors <- err })
	}
	group.Wait()
	close(bindings)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var bindingID uint32
	for row := range bindings {
		if bindingID != 0 {
			require.Equal(t, bindingID, row.Id)
		}
		bindingID = row.Id
		require.False(t, row.Enabled)
	}
	scope, err := repo.Scope(merchant)
	require.NoError(t, err)
	require.Len(t, scope, 1, "pending enrollment exposed trading scope")
	require.NoError(t, repo.CompleteAccountEnrollment(merchant, bindingID))
	scope, err = repo.Scope(merchant)
	require.NoError(t, err)
	require.Len(t, scope, 2)
	otherTenant := uint32(7)
	if winner == 7 {
		otherTenant = 8
	}
	_, err = repo.BeginAccountEnrollment(bindingContext(t.Context(), 3, otherTenant), 100, 201)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}
